-- +goose Up
-- Device identity: one current-state row per enrolled device plus an
-- append-only history of every rotation, suspension, resumption, revocation
-- and site reassignment. History rows are the audit trail; the current row
-- is what ingest and sync check on the hot path.

CREATE TABLE time_device (
    tenant_id text NOT NULL,
    id text NOT NULL,
    public_key bytea NOT NULL,
    site_id text NOT NULL,
    profile_id text NOT NULL,
    timezone text NOT NULL,
    state text NOT NULL DEFAULT 'ACTIVE',
    revision bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    CHECK (state IN ('ACTIVE', 'SUSPENDED', 'REVOKED')),
    CHECK (revision > 0)
);
CREATE INDEX time_device_site_idx ON time_device (tenant_id, site_id);
SELECT time_enable_tenant_isolation('time_device');

CREATE TABLE time_device_history (
    tenant_id text NOT NULL,
    id uuid NOT NULL,
    device_id text NOT NULL,
    revision bigint NOT NULL,
    kind text NOT NULL,
    actor_id text NOT NULL,
    reason text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    CHECK (kind IN ('ENROLLED', 'KEY_ROTATED', 'SUSPENDED', 'RESUMED', 'REVOKED', 'SITE_REASSIGNED'))
);
CREATE UNIQUE INDEX time_device_history_rev_idx ON time_device_history (tenant_id, device_id, revision);
SELECT time_enable_tenant_isolation('time_device_history');
CREATE TRIGGER time_device_history_immutable BEFORE UPDATE OR DELETE ON time_device_history FOR EACH ROW EXECUTE FUNCTION time_forbid_mutation();

-- +goose Down
DROP TABLE time_device_history;
DROP TABLE time_device;
