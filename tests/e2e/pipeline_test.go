//go:build integration

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"diplom/internal/telemetry"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/segmentio/kafka-go"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func TestMQTTKafkaDatabaseAPI(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	base := env("TEST_API_URL", "http://localhost:8080")
	client := &http.Client{Timeout: 3 * time.Second}
	for _, port := range []string{"8080", "8081", "8082"} {
		req, _ := http.NewRequestWithContext(ctx, "GET", "http://localhost:"+port+"/health/ready", nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("service %s not ready: %d", port, resp.StatusCode)
		}
	}
	id := fmt.Sprintf("e2e-%d", time.Now().UnixNano())
	body, _ := json.Marshal(map[string]any{"id": id, "name": "E2E device", "expected_interval_seconds": 1})
	req, _ := http.NewRequestWithContext(ctx, "POST", base+"/api/v1/devices", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("register: %d", resp.StatusCode)
	}
	// Capture the current quarantine offset before publishing the test messages.
	conn, err := kafka.DialLeader(ctx, "tcp", env("TEST_KAFKA_BROKER", "localhost:19092"), "telemetry.invalid.v1", 0)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	offset, err := conn.ReadLastOffset()
	conn.Close()
	if err != nil {
		t.Fatal(err)
	}
	quarantine := kafka.NewReader(kafka.ReaderConfig{Brokers: []string{env("TEST_KAFKA_BROKER", "localhost:19092")}, Topic: "telemetry.invalid.v1", Partition: 0, MinBytes: 1, MaxBytes: 1e6})
	defer quarantine.Close()
	if err = quarantine.SetOffset(offset); err != nil {
		t.Fatal(err)
	}
	mqttClient := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(env("TEST_MQTT_URL", "tcp://localhost:1883")).SetClientID(id))
	waitToken := func(token mqtt.Token) {
		t.Helper()
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-token.Done():
			if err := token.Error(); err != nil {
				t.Fatal(err)
			}
		}
	}
	waitToken(mqttClient.Connect())
	defer mqttClient.Disconnect(250)
	now := time.Now().UTC()
	measurement := telemetry.Measurement{SchemaVersion: 1, MessageID: "message-1", DeviceID: id, SessionID: "e2e-session", Sequence: 1, Timestamp: now, Voltage: 230, Current: 5, ActivePower: 1092.5, PowerFactor: .95, Temperature: 35}
	publish := func(m telemetry.Measurement) {
		t.Helper()
		b, _ := json.Marshal(m)
		waitToken(mqttClient.Publish("devices/"+id+"/telemetry", 1, false, b))
	}
	publish(measurement)
	// Confirm persistence through the public API rather than MQTT PUBACK.
	for {
		req, _ = http.NewRequestWithContext(ctx, "GET", base+"/api/v1/devices/"+id+"/latest", nil)
		resp, err = client.Do(req)
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == 200 {
				var event telemetry.Event
				if err = json.Unmarshal(b, &event); err != nil {
					t.Fatal(err)
				}
				if event.Measurement.MessageID != "message-1" {
					t.Fatal("wrong latest measurement")
				}
				break
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("measurement never reached API")
		case <-time.After(100 * time.Millisecond):
		}
	}
	publish(measurement) // An application-level retry with the same message ID.
	conflict := measurement
	conflict.Temperature = 99
	publish(conflict)
	receivedConflict := false
	for !receivedConflict {
		record, err := quarantine.ReadMessage(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var rejected telemetry.Rejection
		if err = json.Unmarshal(record.Value, &rejected); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(rejected.Payload, id) && rejected.Reason == "message_id_content_conflict" {
			receivedConflict = true
		}
	}
	// Conflict follows the duplicate in the same device partition; seeing its
	// quarantine record means the preceding duplicate has been processed.
	req, _ = http.NewRequestWithContext(ctx, "GET", base+"/api/v1/devices/"+id+"/measurements", nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("history: %d", resp.StatusCode)
	}
	var history struct {
		Items []telemetry.Event `json:"items"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&history); err != nil {
		t.Fatal(err)
	}
	if len(history.Items) != 1 || history.Items[0].Measurement.Temperature != 35 {
		t.Fatalf("duplicate/conflict changed history: %+v", history.Items)
	}
	t.Logf("verified MQTT → Kafka → TimescaleDB → REST API; device=%s; unique=1; duplicate suppressed; conflict quarantined", id)
}
