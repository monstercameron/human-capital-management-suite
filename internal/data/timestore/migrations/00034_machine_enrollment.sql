-- +goose Up
-- TCLOCK-002: durable hand-off between the time store and the core
-- machine-client registry.  The raw enrollment code and private key never
-- cross into this table; only the code hash and public proof material do.
CREATE TABLE time_machine_enrollment_pending (
    tenant_id text NOT NULL,
    device_id text NOT NULL,
    machine_client_id text NOT NULL,
    enrollment_code_hash text NOT NULL,
    owner text NOT NULL,
    site_id text NOT NULL,
    profile_id text NOT NULL,
    timezone text NOT NULL,
    public_key bytea NOT NULL,
    challenge bytea NOT NULL,
    signature bytea NOT NULL,
    key_id text NOT NULL,
    scopes jsonb NOT NULL,
    purpose text NOT NULL,
    expires_at timestamptz NOT NULL,
    idempotency_key text NOT NULL,
    state text NOT NULL DEFAULT 'PENDING',
    credential_ref text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, device_id),
    UNIQUE (tenant_id, enrollment_code_hash),
    UNIQUE (tenant_id, idempotency_key),
    CHECK (state IN ('PENDING', 'ACTIVATED'))
);
CREATE INDEX time_machine_enrollment_pending_state_idx
    ON time_machine_enrollment_pending (tenant_id, state);
SELECT time_enable_tenant_isolation('time_machine_enrollment_pending');

-- +goose Down
DROP TABLE time_machine_enrollment_pending;
