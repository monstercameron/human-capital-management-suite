-- +goose Up
-- Biometric consent records are bound to a notice version and carry a
-- destruction schedule. Only a custody reference to the template is stored
-- here; template bytes never reach this schema. State transitions are
-- revisioned in an append-only history table.

CREATE TABLE time_biometric_consent (
    tenant_id text NOT NULL,
    id uuid NOT NULL,
    worker_id text NOT NULL,
    notice_version text NOT NULL,
    legal_basis text NOT NULL,
    jurisdiction text NOT NULL,
    template_custody_ref text NOT NULL,
    destruction_at timestamptz NOT NULL,
    state text NOT NULL DEFAULT 'GRANTED',
    revision bigint NOT NULL DEFAULT 1,
    consented_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    destroyed_at timestamptz,
    PRIMARY KEY (tenant_id, id),
    CHECK (state IN ('GRANTED', 'REVOKED', 'DESTROYED'))
);
CREATE INDEX time_biometric_consent_worker_idx ON time_biometric_consent (tenant_id, worker_id);
CREATE INDEX time_biometric_consent_destruction_idx ON time_biometric_consent (tenant_id, destruction_at) WHERE state <> 'DESTROYED';
SELECT time_enable_tenant_isolation('time_biometric_consent');

CREATE TABLE time_biometric_consent_history (
    tenant_id text NOT NULL,
    id uuid NOT NULL,
    consent_id uuid NOT NULL,
    revision bigint NOT NULL,
    event text NOT NULL,
    reason text NOT NULL DEFAULT '',
    actor_id text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    CHECK (event IN ('GRANTED', 'REVOKED', 'DESTROYED'))
);
CREATE UNIQUE INDEX time_biometric_consent_history_rev_idx ON time_biometric_consent_history (tenant_id, consent_id, revision);
SELECT time_enable_tenant_isolation('time_biometric_consent_history');
CREATE TRIGGER time_biometric_consent_history_immutable BEFORE UPDATE OR DELETE ON time_biometric_consent_history FOR EACH ROW EXECUTE FUNCTION time_forbid_mutation();

-- +goose Down
DROP TABLE time_biometric_consent_history;
DROP TABLE time_biometric_consent;
