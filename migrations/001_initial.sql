CREATE EXTENSION IF NOT EXISTS timescaledb;

CREATE TABLE devices (
 id text PRIMARY KEY,
 name text NOT NULL,
 expected_interval_seconds integer NOT NULL CHECK (expected_interval_seconds BETWEEN 1 AND 86400),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE processed_messages (
 device_id text NOT NULL REFERENCES devices(id),
 message_id text NOT NULL,
 payload_hash bytea NOT NULL,
 processed_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(device_id,message_id)
);
CREATE TABLE telemetry (
 device_id text NOT NULL REFERENCES devices(id),
 message_id text NOT NULL,
 measured_at timestamptz NOT NULL,
 received_at timestamptz NOT NULL,
 voltage_v double precision NOT NULL,
 current_a double precision NOT NULL,
 active_power_w double precision NOT NULL,
 power_factor double precision NOT NULL,
 temperature_c double precision NOT NULL,
 payload jsonb NOT NULL,
 PRIMARY KEY(device_id,measured_at,message_id)
);
SELECT create_hypertable('telemetry','measured_at',chunk_time_interval => INTERVAL '1 day');
CREATE TABLE device_state (
 device_id text PRIMARY KEY REFERENCES devices(id),
 last_seen_at timestamptz NOT NULL,
 latest_received_at timestamptz NOT NULL,
 measured_at timestamptz NOT NULL,
 message_id text NOT NULL,
 payload jsonb NOT NULL
);
