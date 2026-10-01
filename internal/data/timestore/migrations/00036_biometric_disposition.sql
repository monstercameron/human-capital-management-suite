-- +goose Up
-- TCLOCK-006/017: durable biometric crypto-erasure saga.  The disposition
-- row is the tombstone; key custody remains an injected external system.
CREATE TABLE time_biometric_disposition (
    tenant_id text NOT NULL,
    id uuid NOT NULL,
    consent_id uuid NOT NULL,
    worker_id text NOT NULL,
    template_custody_ref text NOT NULL,
    state text NOT NULL DEFAULT 'TOMBSTONE_PENDING',
    idempotency_key text NOT NULL,
    claim_id uuid,
    tombstoned_at timestamptz,
    key_destroyed_at timestamptz,
    completed_at timestamptz,
    last_error text NOT NULL DEFAULT '',
    revision bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, consent_id),
    UNIQUE (tenant_id, idempotency_key),
    CHECK (state IN ('TOMBSTONE_PENDING','TOMBSTONED','KEY_DESTROYING','KEY_DESTROYED','COMPLETED','HOLD_BLOCKED'))
);
CREATE INDEX time_biometric_disposition_pending_idx ON time_biometric_disposition (tenant_id, state, updated_at);
SELECT time_enable_tenant_isolation('time_biometric_disposition');

CREATE TABLE time_biometric_disposition_event (
    tenant_id text NOT NULL,
    id uuid NOT NULL,
    disposition_id uuid NOT NULL,
    event text NOT NULL,
    actor_id text NOT NULL,
    reason text NOT NULL DEFAULT '',
    claim_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    CHECK (event IN ('WITHDRAWN','TOMBSTONED','HOLD_BLOCKED','KEY_DESTROYED'))
);
SELECT time_enable_tenant_isolation('time_biometric_disposition_event');
CREATE TRIGGER time_biometric_disposition_event_immutable BEFORE UPDATE OR DELETE ON time_biometric_disposition_event FOR EACH ROW EXECUTE FUNCTION time_forbid_mutation();

CREATE TABLE time_biometric_key_destroy_claim (
    tenant_id text NOT NULL,
    id uuid NOT NULL,
    disposition_id uuid NOT NULL,
    custody_ref text NOT NULL,
    state text NOT NULL DEFAULT 'CLAIMED',
    claimed_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, disposition_id),
    CHECK (state IN ('CLAIMED','COMPLETED'))
);
SELECT time_enable_tenant_isolation('time_biometric_key_destroy_claim');

-- +goose Down
DROP TABLE time_biometric_disposition_event;
DROP TABLE time_biometric_key_destroy_claim;
DROP TABLE time_biometric_disposition;
