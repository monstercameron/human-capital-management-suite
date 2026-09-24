-- Owner: AUTHN-002. REV-033-01 persists the short-lived OIDC PKCE transaction
-- across process restarts and replica changes; the opaque pointer enables a
-- callback to discover tenant scope before setting app.tenant_id.
-- +goose Up

CREATE TABLE oidc_pending_authorization (
    tenant_id              tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    tenant_key             text NOT NULL,
    state                  text NOT NULL,
    issuer_url             text NOT NULL,
    client_id              text NOT NULL,
    redirect_uri           text NOT NULL,
    nonce                  text NOT NULL,
    code_challenge_digest  text NOT NULL,
    created_at             timestamptz NOT NULL,
    expires_at             timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, state),
    CHECK (state <> ''),
    CHECK (nonce <> ''),
    CHECK (code_challenge_digest <> ''),
    CHECK (expires_at > created_at)
);

ALTER TABLE oidc_pending_authorization ENABLE ROW LEVEL SECURITY;
ALTER TABLE oidc_pending_authorization FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON oidc_pending_authorization
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, DELETE ON oidc_pending_authorization TO hcmnext_app;

CREATE TABLE oidc_pending_authorization_pointer (
    state       text PRIMARY KEY,
    tenant_id   tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    expires_at  timestamptz NOT NULL
);
CREATE INDEX oidc_pending_authorization_pointer_expiry_idx ON oidc_pending_authorization_pointer (expires_at);
GRANT SELECT, INSERT, DELETE ON oidc_pending_authorization_pointer TO hcmnext_app;

-- +goose Down
DROP TABLE IF EXISTS oidc_pending_authorization_pointer;
DROP TABLE IF EXISTS oidc_pending_authorization;
