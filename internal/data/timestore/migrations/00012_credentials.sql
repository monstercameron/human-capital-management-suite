-- +goose Up
-- Worker credentials for shared devices, plus per-device and per-worker
-- failed-attempt counters and an append-only supervisor-override ledger.

CREATE TABLE time_worker_credential (
    tenant_id text NOT NULL,
    id uuid NOT NULL,
    worker_id text NOT NULL,
    kind text NOT NULL,
    verifier_hash bytea,
    verifier_salt bytea,
    external_id text NOT NULL DEFAULT '',
    state text NOT NULL DEFAULT 'ACTIVE',
    revision bigint NOT NULL DEFAULT 1,
    issued_at timestamptz NOT NULL DEFAULT now(),
    rotated_at timestamptz,
    revoked_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    CHECK (kind IN ('PIN', 'BADGE', 'QR')),
    CHECK (state IN ('ACTIVE', 'REVOKED'))
);
CREATE UNIQUE INDEX time_worker_credential_worker_kind_idx ON time_worker_credential (tenant_id, worker_id, kind);
SELECT time_enable_tenant_isolation('time_worker_credential');

CREATE TABLE time_worker_credential_history (
    tenant_id text NOT NULL,
    id uuid NOT NULL,
    credential_id uuid NOT NULL,
    revision bigint NOT NULL,
    event text NOT NULL,
    actor_id text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    CHECK (event IN ('ISSUED', 'ROTATED', 'REVOKED'))
);
CREATE UNIQUE INDEX time_worker_credential_history_rev_idx ON time_worker_credential_history (tenant_id, credential_id, revision);
SELECT time_enable_tenant_isolation('time_worker_credential_history');
CREATE TRIGGER time_worker_credential_history_immutable BEFORE UPDATE OR DELETE ON time_worker_credential_history FOR EACH ROW EXECUTE FUNCTION time_forbid_mutation();

CREATE TABLE time_device_lockout (
    tenant_id text NOT NULL,
    device_id text NOT NULL,
    failed_count integer NOT NULL DEFAULT 0,
    locked_until timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, device_id)
);
SELECT time_enable_tenant_isolation('time_device_lockout');

CREATE TABLE time_worker_lockout (
    tenant_id text NOT NULL,
    worker_id text NOT NULL,
    failed_count integer NOT NULL DEFAULT 0,
    locked_until timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, worker_id)
);
SELECT time_enable_tenant_isolation('time_worker_lockout');

CREATE TABLE time_supervisor_override (
    tenant_id text NOT NULL,
    id uuid NOT NULL,
    device_id text NOT NULL,
    worker_id text NOT NULL,
    supervisor_credential_ref text NOT NULL,
    reason text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id)
);
SELECT time_enable_tenant_isolation('time_supervisor_override');
CREATE TRIGGER time_supervisor_override_immutable BEFORE UPDATE OR DELETE ON time_supervisor_override FOR EACH ROW EXECUTE FUNCTION time_forbid_mutation();

-- +goose Down
DROP TABLE time_supervisor_override;
DROP TABLE time_worker_lockout;
DROP TABLE time_device_lockout;
DROP TABLE time_worker_credential_history;
DROP TABLE time_worker_credential;
