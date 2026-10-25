package telemetry

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"time"
)

const MaxPayload = 4096

var identifier = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

func ValidID(id string) bool { return identifier.MatchString(id) }

type Measurement struct {
	SchemaVersion int       `json:"schema_version"`
	MessageID     string    `json:"message_id"`
	DeviceID      string    `json:"device_id"`
	SessionID     string    `json:"session_id"`
	Sequence      uint64    `json:"sequence"`
	Timestamp     time.Time `json:"timestamp"`
	Voltage       float64   `json:"voltage_v"`
	Current       float64   `json:"current_a"`
	ActivePower   float64   `json:"active_power_w"`
	PowerFactor   float64   `json:"power_factor"`
	Temperature   float64   `json:"temperature_c"`
}

type Event struct {
	Measurement Measurement `json:"measurement"`
	ReceivedAt  time.Time   `json:"received_at"`
}

type Rejection struct {
	Reason     string    `json:"reason"`
	Topic      string    `json:"topic"`
	Payload    string    `json:"payload"`
	ReceivedAt time.Time `json:"received_at"`
}

func Decode(payload []byte, topic string, now time.Time) (Measurement, error) {
	var m Measurement
	if len(payload) > MaxPayload {
		return m, errors.New("payload_too_large")
	}
	d := json.NewDecoder(bytes.NewReader(payload))
	d.DisallowUnknownFields()
	if err := d.Decode(&m); err != nil {
		return m, fmt.Errorf("invalid_json: %w", err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return m, errors.New("trailing_json")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return m, errors.New("invalid_json")
	}
	for _, key := range []string{"schema_version", "message_id", "device_id", "session_id", "sequence", "timestamp", "voltage_v", "current_a", "active_power_w", "power_factor", "temperature_c"} {
		v, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return m, fmt.Errorf("missing_field: %s", key)
		}
	}
	if err := m.Validate(now); err != nil {
		return m, err
	}
	if topic != "devices/"+m.DeviceID+"/telemetry" {
		return m, errors.New("topic_device_mismatch")
	}
	return m, nil
}

func (m Measurement) Validate(now time.Time) error {
	if m.SchemaVersion != 1 {
		return errors.New("unsupported_schema")
	}
	if !ValidID(m.DeviceID) || !ValidID(m.MessageID) || !ValidID(m.SessionID) {
		return errors.New("invalid_identifier")
	}
	if m.Sequence == 0 || m.Sequence > math.MaxInt64 {
		return errors.New("invalid_sequence")
	}
	if m.Timestamp.IsZero() || m.Timestamp.After(now.Add(30*time.Second)) || m.Timestamp.Before(now.Add(-24*time.Hour)) {
		return errors.New("invalid_timestamp")
	}
	for _, v := range []float64{m.Voltage, m.Current, m.ActivePower, m.PowerFactor, m.Temperature} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return errors.New("non_finite_value")
		}
	}
	if m.Voltage <= 0 || m.Voltage > 1000 || m.Current < 0 || m.Current > 10000 || m.ActivePower < 0 || m.PowerFactor <= 0 || m.PowerFactor > 1 || m.Temperature < -100 || m.Temperature > 300 {
		return errors.New("outside_physical_range")
	}
	expected := m.Voltage * m.Current * m.PowerFactor
	if math.Abs(m.ActivePower-expected) > math.Max(1, expected*0.02) {
		return errors.New("inconsistent_power")
	}
	return nil
}

func (m Measurement) Digest() []byte {
	// Normalize the time zone so equivalent timestamps have the same identity.
	m.Timestamp = m.Timestamp.UTC()
	b, _ := json.Marshal(m)
	digest := sha256.Sum256(b)
	return digest[:]
}
