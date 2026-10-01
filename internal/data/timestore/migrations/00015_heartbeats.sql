-- +goose Up
-- One latest-state row per device, upserted on every heartbeat, plus a
-- bounded append-only history for drift and outage forensics.

CREATE TABLE time_device_heartbeat_latest (
    tenant_id text NOT NULL,
    device_id text NOT NULL,
    last_seen timestamptz NOT NULL,
    app_version text NOT NULL,
    queue_depth integer NOT NULL DEFAULT 0,
    oldest_unsent_age_seconds integer NOT NULL DEFAULT 0,
    battery_percent integer,
    power_state text NOT NULL DEFAULT 'UNKNOWN',
    offset_millis bigint NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, device_id)
);
SELECT time_enable_tenant_isolation('time_device_heartbeat_latest');

CREATE TABLE time_device_heartbeat_history (
    tenant_id text NOT NULL,
    id uuid NOT NULL,
    device_id text NOT NULL,
    observed_at timestamptz NOT NULL,
    app_version text NOT NULL,
    queue_depth integer NOT NULL DEFAULT 0,
    oldest_unsent_age_seconds integer NOT NULL DEFAULT 0,
    battery_percent integer,
    power_state text NOT NULL DEFAULT 'UNKNOWN',
    offset_millis bigint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id)
);
CREATE INDEX time_device_heartbeat_history_device_idx ON time_device_heartbeat_history (tenant_id, device_id, observed_at DESC);
SELECT time_enable_tenant_isolation('time_device_heartbeat_history');
CREATE TRIGGER time_device_heartbeat_history_immutable BEFORE UPDATE OR DELETE ON time_device_heartbeat_history FOR EACH ROW EXECUTE FUNCTION time_forbid_mutation();

-- +goose Down
DROP TABLE time_device_heartbeat_history;
DROP TABLE time_device_heartbeat_latest;
