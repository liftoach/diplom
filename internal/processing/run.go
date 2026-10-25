package processing

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"diplom/internal/platform"
	"diplom/internal/storage"
	"diplom/internal/telemetry"
	"diplom/internal/transport"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/segmentio/kafka-go"
)

func Run(parent context.Context) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	db, err := storage.Open(ctx, platform.Env("DATABASE_URL", "postgres://telemetry:telemetry@localhost:5432/telemetry?sslmode=disable"))
	if err != nil {
		return err
	}
	defer db.Pool.Close()
	reader := kafka.NewReader(kafka.ReaderConfig{Brokers: transport.Brokers(), Topic: platform.Env("KAFKA_TOPIC", "telemetry.valid.v1"), GroupID: platform.Env("KAFKA_GROUP", "telemetry-storage-v1"), MinBytes: 1, MaxBytes: 1e6, MaxWait: 250 * time.Millisecond, CommitInterval: 0, QueueCapacity: 32})
	defer reader.Close()
	writer := transport.Writer()
	defer writer.Close()
	messages := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "processor_messages_total", Help: "Committed Kafka records by processing outcome."}, []string{"outcome"})
	latency := prometheus.NewHistogram(prometheus.HistogramOpts{Name: "processor_commit_latency_seconds", Help: "Ingestion receipt to acknowledged database commit, excluding duplicate messages.", Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60}})
	prometheus.MustRegister(messages, latency)
	done := platform.HTTP(ctx, platform.Env("HTTP_ADDR", ":8082"), http.NewServeMux(), func(c context.Context) error {
		if err := db.Pool.Ping(c); err != nil {
			return err
		}
		return transport.Ready(c)
	})
	worker := make(chan error, 1)
	go func() { worker <- consume(ctx, reader, writer, db, messages, latency) }()
	select {
	case <-ctx.Done():
		<-worker
		return <-done
	case err := <-worker:
		cancel()
		httpErr := <-done
		if err != nil {
			return err
		}
		return httpErr
	case err := <-done:
		cancel()
		<-worker
		return err
	}
}

func consume(ctx context.Context, reader *kafka.Reader, writer *kafka.Writer, db *storage.Store, messages *prometheus.CounterVec, latency prometheus.Histogram) error {
	for ctx.Err() == nil {
		var record kafka.Message
		// An idle topic is normal: FetchMessage waits for data until shutdown.
		if err := platform.Retry(ctx, "kafka fetch", func(_ context.Context) error { var e error; record, e = reader.FetchMessage(ctx); return e }); err != nil {
			return nil
		}
		var event telemetry.Event
		reject := json.Unmarshal(record.Value, &event)
		if reject == nil {
			if event.ReceivedAt.IsZero() || event.ReceivedAt.After(time.Now().Add(30*time.Second)) {
				reject = errors.New("invalid_received_at")
			} else {
				reject = event.Measurement.Validate(event.ReceivedAt)
			}
			if reject == nil && string(record.Key) != event.Measurement.DeviceID {
				reject = errors.New("kafka_key_device_mismatch")
			}
		}
		inserted := false
		if reject == nil {
			if err := platform.Retry(ctx, "database transaction", func(c context.Context) error {
				var e error
				inserted, e = db.Save(c, event)
				if errors.Is(e, storage.ErrConflict) || errors.Is(e, storage.ErrUnknownDevice) {
					reject = e
					return nil
				}
				return e
			}); err != nil {
				return nil
			}
		}
		outcome := "duplicate"
		commitLatency := time.Since(event.ReceivedAt).Seconds()
		if inserted {
			outcome = "inserted"
		}
		if reject != nil {
			outcome = "invalid"
			payload := record.Value
			if len(payload) > telemetry.MaxPayload {
				payload = payload[:telemetry.MaxPayload]
			}
			b, _ := json.Marshal(telemetry.Rejection{Reason: reject.Error(), Topic: record.Topic, Payload: string(payload), ReceivedAt: time.Now().UTC()})
			if err := platform.Retry(ctx, "quarantine publish", func(c context.Context) error {
				return writer.WriteMessages(c, kafka.Message{Topic: platform.Env("KAFKA_INVALID_TOPIC", "telemetry.invalid.v1"), Key: record.Key, Value: b})
			}); err != nil {
				return nil
			}
		}
		if err := platform.Retry(ctx, "offset commit", func(c context.Context) error { return reader.CommitMessages(c, record) }); err != nil {
			return nil
		}
		messages.WithLabelValues(outcome).Inc()
		if inserted {
			latency.Observe(commitLatency)
		}
		slog.Debug("record processed", "partition", record.Partition, "offset", record.Offset, "outcome", outcome)
	}
	return nil
}
