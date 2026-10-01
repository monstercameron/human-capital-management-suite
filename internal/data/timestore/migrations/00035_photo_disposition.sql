-- +goose Up
-- TCLOCK-007 retention disposition.  A claim/tombstone is durable before an
-- external artifact delete is attempted; finalize is retryable after success.
ALTER TABLE time_punch_photo ADD COLUMN IF NOT EXISTS disposition_revision bigint NOT NULL DEFAULT 1;

CREATE TABLE time_punch_photo_hold (
    tenant_id text NOT NULL,
    photo_id uuid NOT NULL,
    hold_ref text NOT NULL,
    placed_at timestamptz NOT NULL DEFAULT now(),
    released_at timestamptz,
    PRIMARY KEY (tenant_id, photo_id, hold_ref),
    FOREIGN KEY (tenant_id, photo_id) REFERENCES time_punch_photo(tenant_id, id)
);
CREATE INDEX time_punch_photo_hold_live ON time_punch_photo_hold(tenant_id, photo_id) WHERE released_at IS NULL;
SELECT time_enable_tenant_isolation('time_punch_photo_hold');

CREATE TABLE time_punch_photo_exception (
    tenant_id text NOT NULL,
    photo_id uuid NOT NULL,
    exception_ref text NOT NULL,
    opened_at timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz,
    PRIMARY KEY (tenant_id, photo_id, exception_ref),
    FOREIGN KEY (tenant_id, photo_id) REFERENCES time_punch_photo(tenant_id, id)
);
CREATE INDEX time_punch_photo_exception_live ON time_punch_photo_exception(tenant_id, photo_id) WHERE resolved_at IS NULL;
SELECT time_enable_tenant_isolation('time_punch_photo_exception');

CREATE TABLE time_punch_photo_disposition (
    tenant_id text NOT NULL,
    photo_id uuid NOT NULL,
    artifact_ref text NOT NULL,
    state text NOT NULL CHECK (state IN ('CLAIMED','TOMBSTONED','DELETED')),
    revision bigint NOT NULL CHECK (revision > 0),
    last_idempotency_key text NOT NULL,
    claimed_at timestamptz NOT NULL DEFAULT now(),
    tombstoned_at timestamptz,
    finalized_at timestamptz,
    PRIMARY KEY (tenant_id, photo_id),
    FOREIGN KEY (tenant_id, photo_id) REFERENCES time_punch_photo(tenant_id, id)
);
SELECT time_enable_tenant_isolation('time_punch_photo_disposition');

CREATE TABLE time_punch_photo_disposition_audit (
    tenant_id text NOT NULL,
    id uuid NOT NULL,
    photo_id uuid NOT NULL,
    action text NOT NULL CHECK (action IN ('CLAIM','TOMBSTONE','FINALIZE')),
    idempotency_key text NOT NULL,
    revision bigint NOT NULL CHECK (revision > 0),
    artifact_ref text NOT NULL,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, photo_id, idempotency_key)
);
CREATE INDEX time_punch_photo_disposition_audit_photo ON time_punch_photo_disposition_audit(tenant_id, photo_id, revision);
CREATE TRIGGER time_punch_photo_disposition_audit_immutable BEFORE UPDATE OR DELETE ON time_punch_photo_disposition_audit FOR EACH ROW EXECUTE FUNCTION time_forbid_mutation();
SELECT time_enable_tenant_isolation('time_punch_photo_disposition_audit');

-- Governance changes serialize with a disposition claim/tombstone. This is
-- the database lease that closes the hold-vs-delete TOCTOU window.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION time_photo_governance_lock() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE photo uuid;
DECLARE disposition text;
BEGIN
    photo := COALESCE((to_jsonb(NEW)->>'id')::uuid, (to_jsonb(NEW)->>'photo_id')::uuid);
    PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id || ':' || photo::text, 0));
    SELECT state INTO disposition FROM time_punch_photo_disposition WHERE tenant_id=NEW.tenant_id AND photo_id=photo FOR UPDATE;
    IF disposition IN ('TOMBSTONED','DELETED') THEN
        RAISE EXCEPTION 'photo disposition is fenced by %', disposition USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER time_punch_photo_hold_lock BEFORE INSERT OR UPDATE ON time_punch_photo_hold FOR EACH ROW EXECUTE FUNCTION time_photo_governance_lock();
CREATE TRIGGER time_punch_photo_exception_lock BEFORE INSERT OR UPDATE ON time_punch_photo_exception FOR EACH ROW EXECUTE FUNCTION time_photo_governance_lock();
CREATE TRIGGER time_punch_photo_legacy_hold_lock BEFORE UPDATE OF legal_hold ON time_punch_photo FOR EACH ROW EXECUTE FUNCTION time_photo_governance_lock();

-- +goose Down
DROP TABLE time_punch_photo_disposition_audit;
DROP TABLE time_punch_photo_disposition;
DROP TABLE time_punch_photo_exception;
DROP TABLE time_punch_photo_hold;
ALTER TABLE time_punch_photo DROP COLUMN disposition_revision;
