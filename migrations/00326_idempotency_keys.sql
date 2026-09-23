-- INTAPI-005: durable idempotency-key record for external writes. The
-- first claim of a key wins and executes; a repeat with the same request
-- digest replays the stored result; a repeat with a different digest is
-- refused. One row per (tenant, capability, key): the capability owns the
-- key namespace and the expiry, so a key can never replay across
-- capabilities and a completed key is reusable only after its TTL.
--
-- Storage disposition (STORE-001):
--   idempotency_key   CONTROL   OPERATIONAL claim/complete lifecycle record
--
-- Tenant scoped and fail-closed without app.tenant_id like every data
-- table. Rows mutate exactly once (in_progress to completed) and are
-- reclaimed on expiry: unlike the write-once machine_token_use table there
-- is no forbid_mutation trigger, because reclaiming an expired key and
-- sweeping expired rows are the documented lifecycle. DELETE stays granted
-- to hcmnext_app for that reason; the endpoint layer (not this table)
-- decides when a sweep runs.
--
-- +goose Up

CREATE TABLE IF NOT EXISTS idempotency_key (
    tenant_id      tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    row_id         uuid         NOT NULL DEFAULT gen_random_uuid(),
    capability     semantic_key NOT NULL,
    key            text         NOT NULL,
    request_digest text         NOT NULL,
    status         text         NOT NULL DEFAULT 'in_progress',
    result         bytea,
    expires_at     timestamptz  NOT NULL,
    created_at     timestamptz  NOT NULL DEFAULT now(),
    completed_at   timestamptz,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT idempotency_key_unique UNIQUE (tenant_id, capability, key),
    CONSTRAINT idempotency_key_capability_not_blank CHECK (capability <> ''),
    CONSTRAINT idempotency_key_key_not_blank CHECK (key <> ''),
    CONSTRAINT idempotency_key_digest_not_blank CHECK (request_digest <> ''),
    CONSTRAINT idempotency_key_status_known CHECK (status IN ('in_progress', 'completed'))
);

CREATE INDEX IF NOT EXISTS idempotency_key_expiry
    ON idempotency_key (tenant_id, expires_at);

-- DB-017: the same fail-closed tenant predicate as every data table.
-- +goose StatementBegin
DO $$
BEGIN
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', 'idempotency_key');
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', 'idempotency_key');
    EXECUTE format(
        'CREATE POLICY tenant_isolation ON %I '
        'USING (tenant_id = NULLIF(current_setting(''app.tenant_id'', true), '''')::uuid) '
        'WITH CHECK (tenant_id = NULLIF(current_setting(''app.tenant_id'', true), '''')::uuid)',
        'idempotency_key');
END;
$$;
-- +goose StatementEnd

GRANT SELECT, INSERT, UPDATE, DELETE ON idempotency_key TO hcmnext_app;

-- +goose Down

REVOKE ALL ON idempotency_key FROM hcmnext_app;

DROP TABLE idempotency_key;
