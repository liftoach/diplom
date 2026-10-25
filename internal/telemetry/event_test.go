package telemetry

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func sample(now time.Time) Measurement {
	return Measurement{SchemaVersion: 1, MessageID: "msg-1", DeviceID: "meter-1", SessionID: "session-1", Sequence: 1, Timestamp: now, Voltage: 230, Current: 5, ActivePower: 1092.5, PowerFactor: .95, Temperature: 35}
}

func TestValidation(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		change func(*Measurement)
		want   string
	}{
		{"valid", func(*Measurement) {}, ""},
		{"future", func(m *Measurement) { m.Timestamp = now.Add(31 * time.Second) }, "invalid_timestamp"},
		{"old", func(m *Measurement) { m.Timestamp = now.Add(-25 * time.Hour) }, "invalid_timestamp"},
		{"missing time", func(m *Measurement) { m.Timestamp = time.Time{} }, "invalid_timestamp"},
		{"inconsistent power", func(m *Measurement) { m.ActivePower = 5000 }, "inconsistent_power"},
		{"nan", func(m *Measurement) { m.Voltage = math.NaN() }, "non_finite_value"},
		{"negative current", func(m *Measurement) { m.Current = -1 }, "outside_physical_range"},
		{"sequence overflow", func(m *Measurement) { m.Sequence = math.MaxUint64 }, "invalid_sequence"},
		{"topic characters", func(m *Measurement) { m.DeviceID = "meter/+" }, "invalid_identifier"},
		{"overvoltage is valid", func(m *Measurement) { m.Voltage = 280; m.ActivePower = 280 * m.Current * m.PowerFactor }, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := sample(now)
			tc.change(&m)
			err := m.Validate(now)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || err.Error() != tc.want {
				t.Fatalf("got %v, want %s", err, tc.want)
			}
		})
	}
}

func TestDecode(t *testing.T) {
	now := time.Now().UTC()
	b, _ := json.Marshal(sample(now))
	if _, err := Decode(b, "devices/meter-1/telemetry", now); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		payload []byte
		topic   string
	}{
		{"mismatch", b, "devices/meter-2/telemetry"},
		{"trailing", append(append([]byte{}, b...), []byte(` {}`)...), "devices/meter-1/telemetry"},
		{"oversized", []byte(strings.Repeat("x", MaxPayload+1)), "devices/meter-1/telemetry"},
		{"unknown field", []byte(strings.TrimSuffix(string(b), "}") + `,"unexpected":1}`), "devices/meter-1/telemetry"},
		{"missing temperature", []byte(strings.Replace(string(b), `,"temperature_c":35`, "", 1)), "devices/meter-1/telemetry"},
		{"null current", []byte(strings.Replace(string(b), `"current_a":5`, `"current_a":null`, 1)), "devices/meter-1/telemetry"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Decode(tc.payload, tc.topic, now); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestDigestNormalizesTimezone(t *testing.T) {
	now := time.Now().UTC()
	a := sample(now)
	b := a
	b.Timestamp = now.In(time.FixedZone("MSK", 3*3600))
	if string(a.Digest()) != string(b.Digest()) {
		t.Fatal("same instant must have same digest")
	}
	b.Temperature++
	if string(a.Digest()) == string(b.Digest()) {
		t.Fatal("changed content must change digest")
	}
}
