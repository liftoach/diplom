//go:build integration

package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"diplom/internal/telemetry"
)

func TestIdempotencyAndOrdering(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("TEST_DATABASE_URL must point to the migrated test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Pool.Close()
	id := fmt.Sprintf("test-%d", time.Now().UnixNano())
	if _, err = db.Register(ctx, Device{ID: id, Name: "integration", ExpectedIntervalSeconds: 1}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		c, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		for _, table := range []string{"device_state", "telemetry", "processed_messages"} {
			_, _ = db.Pool.Exec(c, "DELETE FROM "+table+" WHERE device_id=$1", id)
		}
		_, _ = db.Pool.Exec(c, "DELETE FROM devices WHERE id=$1", id)
	}()
	now := time.Now().UTC().Truncate(time.Microsecond)
	event := telemetry.Event{Measurement: telemetry.Measurement{SchemaVersion: 1, DeviceID: id, MessageID: "message-1", SessionID: "session-1", Sequence: 1, Timestamp: now, Voltage: 230, Current: 5, PowerFactor: .95, ActivePower: 1092.5, Temperature: 35}, ReceivedAt: now}
	var inserted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, e := db.Save(ctx, event)
			if e != nil {
				t.Error(e)
			}
			if ok {
				inserted.Add(1)
			}
		}()
	}
	wg.Wait()
	if inserted.Load() != 1 {
		t.Fatalf("inserted %d times", inserted.Load())
	}
	duplicate := event
	duplicate.ReceivedAt = now.Add(time.Minute)
	if ok, err := db.Save(ctx, duplicate); err != nil || ok {
		t.Fatalf("duplicate: %v %v", ok, err)
	}
	device, err := db.Device(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if device.LastSeenAt == nil || !device.LastSeenAt.Equal(now) {
		t.Fatal("duplicate changed last_seen")
	}
	conflict := event
	conflict.Measurement.Temperature++
	if _, err := db.Save(ctx, conflict); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected content conflict, got %v", err)
	}
	late := event
	late.Measurement.MessageID = "message-late"
	late.Measurement.Timestamp = now.Add(-time.Minute)
	late.ReceivedAt = now.Add(time.Second)
	if _, err = db.Save(ctx, late); err != nil {
		t.Fatal(err)
	}
	latest, err := db.Latest(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Measurement.MessageID != "message-1" || !latest.ReceivedAt.Equal(now) {
		t.Fatalf("late event replaced latest: %+v", latest)
	}
	history, err := db.History(ctx, id, now.Add(-time.Hour), now.Add(time.Hour), now.Add(time.Hour), "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 {
		t.Fatalf("got %d measurements", len(history))
	}
	next, err := db.History(ctx, id, now.Add(-time.Hour), now.Add(time.Hour), history[0].Measurement.Timestamp, history[0].Measurement.MessageID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(next) != 1 || next[0].Measurement.MessageID != "message-late" {
		t.Fatal("cursor did not advance correctly")
	}
	unknown := event
	unknown.Measurement.DeviceID = id + "-missing"
	if _, err = db.Save(ctx, unknown); !errors.Is(err, ErrUnknownDevice) {
		t.Fatalf("expected unknown device: %v", err)
	}
}
