package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"time"

	"diplom/internal/platform"
	"diplom/internal/simulation"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/prometheus/client_golang/prometheus"
)

func identifier() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func run() error {
	count := flag.Int("devices", 1, "Number of virtual devices (one shared MQTT connection)")
	interval := flag.Duration("interval", time.Second, "Interval between rounds, >= 1s")
	rounds := flag.Int("rounds", 0, "Rounds; 0 runs until interrupted")
	seed := flag.Uint64("seed", 42, "Reproducible physical-model random seed")
	flag.Parse()
	if *count < 1 || *count > 100000 || *interval < time.Second || *interval > 24*time.Hour || *rounds < 0 {
		return errors.New("invalid simulator configuration")
	}
	ctx, cancel := platform.Context()
	defer cancel()
	api := platform.Env("API_URL", "http://localhost:8080")
	httpClient := &http.Client{Timeout: 5 * time.Second}
	for i := 0; i < *count; i++ {
		id := fmt.Sprintf("meter-%06d", i+1)
		body, _ := json.Marshal(map[string]any{"id": id, "name": id, "expected_interval_seconds": int(math.Ceil(interval.Seconds()))})
		if err := platform.Retry(ctx, "register device", func(c context.Context) error {
			req, err := http.NewRequestWithContext(c, http.MethodPost, api+"/api/v1/devices", bytes.NewReader(body))
			if err != nil {
				return err
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := httpClient.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			if resp.StatusCode != 201 && resp.StatusCode != 409 {
				return fmt.Errorf("register device: HTTP %d", resp.StatusCode)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	session := identifier()
	client := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(platform.Env("MQTT_URL", "tcp://localhost:1883")).SetClientID("sim-" + session).SetConnectTimeout(5 * time.Second).SetAutoReconnect(true))
	token := client.Connect()
	select {
	case <-ctx.Done():
		return nil
	case <-token.Done():
		if err := token.Error(); err != nil {
			return err
		}
	}
	defer client.Disconnect(250)
	counter := prometheus.NewCounter(prometheus.CounterOpts{Name: "simulator_published_total", Help: "Messages acknowledged by the MQTT broker; not a database delivery confirmation."})
	duration := prometheus.NewHistogram(prometheus.HistogramOpts{Name: "simulator_round_duration_seconds", Help: "Time to generate and publish one complete round."})
	prometheus.MustRegister(counter, duration)
	done := platform.HTTP(ctx, platform.Env("HTTP_ADDR", ":8083"), http.NewServeMux(), func(context.Context) error {
		if !client.IsConnectionOpen() {
			return errors.New("mqtt unavailable")
		}
		return nil
	})
	httpFinished := false
	defer func() {
		cancel()
		if !httpFinished {
			<-done
		}
	}()
	models := make([]*simulation.Model, *count)
	for i := range models {
		models[i] = simulation.New(*seed, i)
	}
	ticker := time.NewTicker(*interval)
	defer ticker.Stop()
	for round := 0; *rounds == 0 || round < *rounds; round++ {
		started := time.Now()
		for i, model := range models {
			id := fmt.Sprintf("meter-%06d", i+1)
			m := model.Next(id, session, identifier(), uint64(round+1), time.Now(), *interval)
			payload, _ := json.Marshal(m)
			// Retry the same immutable message if a publish is explicitly failed.
			if err := platform.Retry(ctx, "mqtt publish", func(c context.Context) error {
				token := client.Publish("devices/"+id+"/telemetry", 1, false, payload)
				// A timed-out wait does not cancel an in-flight MQTT publish; do not
				// create another publish just because an operation deadline elapsed.
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-token.Done():
					return token.Error()
				}
			}); err != nil {
				return nil
			}
			counter.Inc()
		}
		duration.Observe(time.Since(started).Seconds())
		slog.Info("round published", "round", round+1, "devices", *count, "duration", time.Since(started))
		if *rounds > 0 && round+1 == *rounds {
			break
		}
		select {
		case <-ctx.Done():
			return nil
		case err := <-done:
			httpFinished = true
			return err
		case <-ticker.C:
		}
	}
	return nil
}
func main() {
	if err := run(); err != nil {
		slog.Error("simulator stopped", "error", err)
		os.Exit(1)
	}
}
