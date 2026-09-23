-- INTAPI-001: machine-client registry for the OAuth 2.0 client-credentials
-- token endpoint. External systems authenticate with a private_key_jwt
-- client assertion or a bound mutual-TLS certificate and receive a
-- short-lived (at most fifteen minutes) asymmetric token; the registry below
-- is the server-side authority the token endpoint joins the client against.
--
-- Storage disposition (STORE-001):
--   machine_client      CONTROL   OPERATIONAL caller-driven registration state
--   machine_client_key  CONTROL   OPERATIONAL rotation-fenced key state
--
-- Both relations are caller-driven state and retain UPDATE but never DELETE
-- (DELETE is revoked from PUBLIC and never granted to hcmnext_app). Both
-- are tenant scoped and fail closed without the transaction-local
-- app.tenant_id set by internal/data/tenancy.WithTenant. A client with an
-- empty ip_allowlist admits no source address: fail closed, never open.
--
-- +goose Up

CREATE TABLE IF NOT EXISTS machine_client (
    tenant_id    tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    row_id       uuid        NOT NULL DEFAULT gen_random_uuid(),
    client_id    semantic_key NOT NULL,
    owner        text        NOT NULL,
    status       text        NOT NULL DEFAULT 'active',
    scopes       jsonb       NOT NULL DEFAULT '[]'::jsonb,
    purpose      text        NOT NULL,
    data_domains jsonb       NOT NULL DEFAULT '[]'::jsonb,
    field_subset jsonb       NOT NULL DEFAULT '[]'::jsonb,
    ip_allowlist jsonb       NOT NULL DEFAULT '[]'::jsonb,
    cert_fingerprint text,
    expires_at   timestamptz,
    revoked      boolean     NOT NULL DEFAULT false,
    last_used_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT machine_client_id_unique UNIQUE (tenant_id, client_id),
    CONSTRAINT machine_client_id_not_blank CHECK (client_id <> ''),
    CONSTRAINT machine_client_cert_fingerprint_shape CHECK (
        cert_fingerprint IS NULL OR cert_fingerprint ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT machine_client_owner_not_blank CHECK (owner <> ''),
    CONSTRAINT machine_client_status_known CHECK (status IN ('active', 'suspended', 'revoked')),
    CONSTRAINT machine_client_purpose_not_blank CHECK (purpose <> ''),
    CONSTRAINT machine_client_expiry_valid CHECK (expires_at IS NULL OR expires_at > created_at)
);

CREATE INDEX IF NOT EXISTS machine_client_current_lookup
    ON machine_client (tenant_id, client_id);

CREATE TABLE IF NOT EXISTS machine_client_key (
    tenant_id   tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    row_id      uuid        NOT NULL DEFAULT gen_random_uuid(),
    client_id   semantic_key NOT NULL,
    kid         text        NOT NULL,
    alg         text        NOT NULL,
    public_jwk  jsonb       NOT NULL,
    not_before  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz,
    revoked     boolean     NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT machine_client_key_id_unique UNIQUE (tenant_id, client_id, kid),
    CONSTRAINT machine_client_key_kid_not_blank CHECK (kid <> ''),
    CONSTRAINT machine_client_key_alg_known CHECK (alg IN ('EdDSA', 'RS256', 'ES256')),
    CONSTRAINT machine_client_key_window_valid CHECK (expires_at IS NULL OR expires_at > not_before),
    CONSTRAINT machine_client_key_client_present
        FOREIGN KEY (tenant_id, client_id) REFERENCES machine_client (tenant_id, client_id)
);

CREATE INDEX IF NOT EXISTS machine_client_key_current_lookup
    ON machine_client_key (tenant_id, client_id, kid);

-- DB-017: both relations use the same fail-closed tenant predicate.
-- +goose StatementBegin
DO $$
DECLARE
    target text;
BEGIN
    FOREACH target IN ARRAY ARRAY[
        'machine_client',
        'machine_client_key'
    ] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', target);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', target);
        EXECUTE format(
            'CREATE POLICY tenant_isolation ON %I '
            'USING (tenant_id = NULLIF(current_setting(''app.tenant_id'', true), '''')::uuid) '
            'WITH CHECK (tenant_id = NULLIF(current_setting(''app.tenant_id'', true), '''')::uuid)',
            target);
        EXECUTE format('REVOKE DELETE ON %I FROM PUBLIC', target);
    END LOOP;
END;
$$;
-- +goose StatementEnd

GRANT SELECT, INSERT, UPDATE ON
    machine_client,
    machine_client_key
TO hcmnext_app;

-- +goose Down

REVOKE ALL ON
    machine_client_key,
    machine_client
FROM hcmnext_app;

DROP TABLE machine_client_key;
DROP TABLE machine_client;
