package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"diplom/internal/telemetry"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrConflict = errors.New("message_id_content_conflict")
var ErrUnknownDevice = errors.New("unknown_device")
var ErrNotFound = errors.New("not_found")

type Store struct{ Pool *pgxpool.Pool }

func Open(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 10
	cfg.MinConns = 0
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &Store{pool}, nil
}

type Device struct {
	ID                      string     `json:"id"`
	Name                    string     `json:"name"`
	ExpectedIntervalSeconds int        `json:"expected_interval_seconds"`
	CreatedAt               time.Time  `json:"created_at"`
	LastSeenAt              *time.Time `json:"last_seen_at"`
}

func (s *Store) Exists(ctx context.Context, id string) (bool, error) {
	var exists bool
	err := s.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM devices WHERE id=$1)", id).Scan(&exists)
	return exists, err
}

func (s *Store) Register(ctx context.Context, d Device) (Device, error) {
	err := s.Pool.QueryRow(ctx, `INSERT INTO devices(id,name,expected_interval_seconds) VALUES($1,$2,$3)
 ON CONFLICT(id) DO NOTHING RETURNING created_at`, d.ID, d.Name, d.ExpectedIntervalSeconds).Scan(&d.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ErrConflict
	}
	return d, err
}

func (s *Store) Devices(ctx context.Context, after string, limit int) ([]Device, error) {
	rows, err := s.Pool.Query(ctx, `SELECT d.id,d.name,d.expected_interval_seconds,d.created_at,s.last_seen_at
 FROM devices d LEFT JOIN device_state s ON d.id=s.device_id WHERE d.id>$1 ORDER BY d.id LIMIT $2`, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	devices := []Device{}
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.ID, &d.Name, &d.ExpectedIntervalSeconds, &d.CreatedAt, &d.LastSeenAt); err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}

func (s *Store) Device(ctx context.Context, id string) (Device, error) {
	var d Device
	err := s.Pool.QueryRow(ctx, `SELECT d.id,d.name,d.expected_interval_seconds,d.created_at,s.last_seen_at
 FROM devices d LEFT JOIN device_state s ON d.id=s.device_id WHERE d.id=$1`, id).Scan(&d.ID, &d.Name, &d.ExpectedIntervalSeconds, &d.CreatedAt, &d.LastSeenAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ErrNotFound
	}
	return d, err
}

func (s *Store) Save(ctx context.Context, e telemetry.Event) (inserted bool, err error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = tx.Rollback(c)
	}()
	var exists bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM devices WHERE id=$1)", e.Measurement.DeviceID).Scan(&exists); err != nil {
		return false, err
	}
	if !exists {
		return false, ErrUnknownDevice
	}
	m := e.Measurement
	tag, err := tx.Exec(ctx, `INSERT INTO processed_messages(device_id,message_id,payload_hash) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, m.DeviceID, m.MessageID, m.Digest())
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		var digest []byte
		if err = tx.QueryRow(ctx, `SELECT payload_hash FROM processed_messages WHERE device_id=$1 AND message_id=$2`, m.DeviceID, m.MessageID).Scan(&digest); err != nil {
			return false, err
		}
		if !bytes.Equal(digest, m.Digest()) {
			return false, ErrConflict
		}
		return false, tx.Commit(ctx)
	}
	body, err := json.Marshal(m)
	if err != nil {
		return false, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO telemetry(device_id,message_id,measured_at,received_at,voltage_v,current_a,active_power_w,power_factor,temperature_c,payload)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, m.DeviceID, m.MessageID, m.Timestamp, e.ReceivedAt, m.Voltage, m.Current, m.ActivePower, m.PowerFactor, m.Temperature, body)
	if err != nil {
		return false, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO device_state(device_id,last_seen_at,measured_at,message_id,payload,latest_received_at) VALUES($1,$2,$3,$4,$5,$2)
 ON CONFLICT(device_id) DO UPDATE SET
 last_seen_at=GREATEST(device_state.last_seen_at,EXCLUDED.last_seen_at),
 latest_received_at=CASE WHEN (EXCLUDED.measured_at,EXCLUDED.message_id)>(device_state.measured_at,device_state.message_id) THEN EXCLUDED.latest_received_at ELSE device_state.latest_received_at END,
 payload=CASE WHEN (EXCLUDED.measured_at,EXCLUDED.message_id)>(device_state.measured_at,device_state.message_id) THEN EXCLUDED.payload ELSE device_state.payload END,
 message_id=CASE WHEN (EXCLUDED.measured_at,EXCLUDED.message_id)>(device_state.measured_at,device_state.message_id) THEN EXCLUDED.message_id ELSE device_state.message_id END,
 measured_at=GREATEST(device_state.measured_at,EXCLUDED.measured_at)`, m.DeviceID, e.ReceivedAt, m.Timestamp, m.MessageID, body)
	if err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (s *Store) Latest(ctx context.Context, id string) (telemetry.Event, error) {
	var e telemetry.Event
	var b []byte
	err := s.Pool.QueryRow(ctx, "SELECT payload,latest_received_at FROM device_state WHERE device_id=$1", id).Scan(&b, &e.ReceivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return e, ErrNotFound
	}
	if err != nil {
		return e, err
	}
	err = json.Unmarshal(b, &e.Measurement)
	return e, err
}

func (s *Store) History(ctx context.Context, id string, from, to, before time.Time, beforeID string, limit int) ([]telemetry.Event, error) {
	rows, err := s.Pool.Query(ctx, `SELECT payload,received_at FROM telemetry WHERE device_id=$1 AND measured_at >= $2 AND measured_at < $3 AND (measured_at,message_id)<($4,$5) ORDER BY measured_at DESC,message_id DESC LIMIT $6`, id, from, to, before, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []telemetry.Event{}
	for rows.Next() {
		var e telemetry.Event
		var b []byte
		if err = rows.Scan(&b, &e.ReceivedAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &e.Measurement); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
