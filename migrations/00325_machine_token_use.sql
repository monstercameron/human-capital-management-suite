-- INTAPI-002: single-use record for machine access-token identifiers. The
-- first presentation of a token identifier records it; a second
-- presentation while it is unexpired is a replay and is refused. Rows are
-- write-once: SELECT/INSERT only, never UPDATE, never DELETE.
--
-- Storage disposition (STORE-001):
--   machine_token_use   LEDGER    OPERATIONAL write-once single-use record
--
-- Tenant scoped and fail-closed without app.tenant_id like every trust
-- table. Rows outlive their usefulness once expired; no retention job
-- exists yet, so expiry only bounds the replay window, not storage growth
-- (a retention follow-up owns that).
--
-- +goose Up

CREATE TABLE IF NOT EXISTS machine_token_use (
    tenant_id  tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    row_id     uuid        NOT NULL DEFAULT gen_random_uuid(),
    token_jti  text        NOT NULL,
    client_id  semantic_key NOT NULL,
    session_ref text       NOT NULL,
    expires_at timestamptz NOT NULL,
    used_at    timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT machine_token_use_jti_unique UNIQUE (tenant_id, token_jti),
    CONSTRAINT machine_token_use_jti_not_blank CHECK (token_jti <> ''),
    CONSTRAINT machine_token_use_client_not_blank CHECK (client_id <> ''),
    CONSTRAINT machine_token_use_session_not_blank CHECK (session_ref <> '')
);

CREATE INDEX IF NOT EXISTS machine_token_use_expiry
    ON machine_token_use (tenant_id, expires_at);

CREATE OR REPLACE TRIGGER machine_token_use_append_only
    BEFORE UPDATE OR DELETE ON machine_token_use
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON machine_token_use FROM PUBLIC;

-- DB-017: the same fail-closed tenant predicate as every trust table.
-- +goose StatementBegin
DO $$
BEGIN
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', 'machine_token_use');
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', 'machine_token_use');
    EXECUTE format(
        'CREATE POLICY tenant_isolation ON %I '
        'USING (tenant_id = NULLIF(current_setting(''app.tenant_id'', true), '''')::uuid) '
        'WITH CHECK (tenant_id = NULLIF(current_setting(''app.tenant_id'', true), '''')::uuid)',
        'machine_token_use');
    EXECUTE format('REVOKE DELETE ON %I FROM PUBLIC', 'machine_token_use');
END;
$$;
-- +goose StatementEnd

GRANT SELECT, INSERT ON machine_token_use TO hcmnext_app;

-- +goose Down

REVOKE ALL ON machine_token_use FROM hcmnext_app;

DROP TABLE machine_token_use;
