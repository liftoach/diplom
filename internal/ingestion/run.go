package ingestion

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"diplom/internal/platform"
	"diplom/internal/storage"
	"diplom/internal/telemetry"
	"diplom/internal/transport"

	mqtt "github.com/eclipse/paho.mqtt.golang"
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
	writer := transport.Writer()
	defer writer.Close()
	messages := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "ingestion_messages_total", Help: "MQTT messages durably published to Kafka by outcome."}, []string{"outcome"})
	prometheus.MustRegister(messages)
	var subscribed atomic.Bool
	opts := mqtt.NewClientOptions().AddBroker(platform.Env("MQTT_URL", "tcp://localhost:1883")).SetClientID(platform.Env("MQTT_CLIENT_ID", "ingestion-1")).SetCleanSession(false).SetAutoAckDisabled(true).SetOrderMatters(true).SetConnectTimeout(5 * time.Second).SetAutoReconnect(true)
	handler := func(_ mqtt.Client, msg mqtt.Message) {
		if ctx.Err() != nil {
			return
		}
		now := time.Now().UTC()
		m, validationErr := telemetry.Decode(msg.Payload(), msg.Topic(), now)
		if validationErr == nil {
			if err := platform.Retry(ctx, "device lookup", func(c context.Context) error {
				exists, e := db.Exists(c, m.DeviceID)
				if e != nil {
					return e
				}
				if !exists {
					validationErr = storage.ErrUnknownDevice
				}
				return nil
			}); err != nil {
				return
			}
		}
		topic := platform.Env("KAFKA_TOPIC", "telemetry.valid.v1")
		outcome := "valid"
		var value []byte
		if validationErr != nil {
			topic = platform.Env("KAFKA_INVALID_TOPIC", "telemetry.invalid.v1")
			outcome = "invalid"
			payload := msg.Payload()
			if len(payload) > telemetry.MaxPayload {
				payload = payload[:telemetry.MaxPayload]
			}
			value, _ = json.Marshal(telemetry.Rejection{Reason: validationErr.Error(), Topic: msg.Topic(), Payload: string(payload), ReceivedAt: now})
		} else {
			value, _ = json.Marshal(telemetry.Event{Measurement: m, ReceivedAt: now})
		}
		err := platform.Retry(ctx, "kafka publish", func(c context.Context) error {
			return writer.WriteMessages(c, kafka.Message{Topic: topic, Key: []byte(m.DeviceID), Value: value})
		})
		if err != nil {
			return
		}
		msg.Ack()
		messages.WithLabelValues(outcome).Inc()
	}
	opts.SetDefaultPublishHandler(handler)
	opts.SetConnectionLostHandler(func(_ mqtt.Client, e error) { subscribed.Store(false); slog.Warn("mqtt disconnected", "error", e) })
	opts.SetOnConnectHandler(func(client mqtt.Client) {
		token := client.Subscribe("devices/+/telemetry", 1, handler)
		select {
		case <-ctx.Done():
			return
		case <-token.Done():
		}
		if token.Error() != nil {
			slog.Error("mqtt subscribe", "error", token.Error())
			return
		}
		subscribed.Store(true)
	})
	client := mqtt.NewClient(opts)
	if err := platform.Retry(ctx, "mqtt connect", func(c context.Context) error {
		token := client.Connect()
		select {
		case <-c.Done():
			return c.Err()
		case <-token.Done():
			return token.Error()
		}
	}); err != nil {
		return err
	}
	defer func() { cancel(); client.Disconnect(250) }()
	done := platform.HTTP(ctx, platform.Env("HTTP_ADDR", ":8081"), http.NewServeMux(), func(c context.Context) error {
		if !subscribed.Load() || !client.IsConnectionOpen() {
			return errors.New("mqtt unavailable")
		}
		if err := db.Pool.Ping(c); err != nil {
			return err
		}
		return transport.Ready(c)
	})
	select {
	case <-ctx.Done():
		return <-done
	case err := <-done:
		return err
	}
}
