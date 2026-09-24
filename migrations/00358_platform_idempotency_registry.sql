-- REV-060-01: durable cross-layer idempotency lifecycle. This registry is
-- intentionally separate from TX-006's transaction-bound idempotency_record.
-- The uniqueness scope includes semantic effect scope and optional principal.
-- Expiry compacts references; permanent tombstones continue to reject reuse.
--
-- +goose Up

CREATE TABLE platform_idempotency_record (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    capability text NOT NULL CHECK (btrim(capability) <> ''),
    effect_scope text NOT NULL CHECK (btrim(effect_scope) <> ''),
    idempotency_key text NOT NULL CHECK (btrim(idempotency_key) <> ''),
    principal text NOT NULL DEFAULT '',
    request_digest char(64) NOT NULL CHECK (request_digest ~ '^[0-9a-f]{64}$'),
    state text NOT NULL CHECK (state IN ('IN_PROGRESS', 'COMPLETED', 'EXPIRED', 'TOMBSTONE')),
    layer text NOT NULL DEFAULT '',
    execution_ref text NOT NULL DEFAULT '',
    result_ref text NOT NULL DEFAULT '',
    effect_ref text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    replay_count bigint NOT NULL DEFAULT 0 CHECK (replay_count >= 0),
    expiry_mode text NOT NULL CHECK (expiry_mode IN ('REJECT_REUSE', 'ALLOW_REUSE')),
    tombstone boolean NOT NULL DEFAULT false,
    PRIMARY KEY (tenant_id, capability, effect_scope, idempotency_key, principal)
);

CREATE INDEX platform_idempotency_record_expiry
    ON platform_idempotency_record (tenant_id, expires_at);

ALTER TABLE platform_idempotency_record ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform_idempotency_record FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON platform_idempotency_record
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE, DELETE ON platform_idempotency_record TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM platform_idempotency_record) THEN
        RAISE EXCEPTION 'cannot remove durable idempotency records';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE platform_idempotency_record;
