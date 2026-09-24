-- REV-058-01: current tenant-owned email sending-domain profile and the
-- freshness fence used by the scheduled DNS authentication check.

-- +goose Up
CREATE TABLE sending_domain_profile (
    tenant_id uuid NOT NULL REFERENCES tenant (tenant_id),
    domain text NOT NULL CHECK (domain = lower(domain) AND domain !~ '[@/[:space:]]' AND position('.' in domain) > 0),
    owner text NOT NULL CHECK (btrim(owner) <> ''),
    profile jsonb NOT NULL CHECK (jsonb_typeof(profile) = 'object'),
    active boolean NOT NULL DEFAULT true,
    verified boolean NOT NULL,
    last_checked_at timestamptz NOT NULL,
    alert_cycle text CHECK (alert_cycle IS NULL OR length(alert_cycle) <= 40),
    PRIMARY KEY (tenant_id, domain),
    CHECK (profile->>'domain' = domain),
    CHECK (profile->>'digest' ~ '^[0-9a-f]{64}$'),
    CHECK (profile->>'record_digest' ~ '^[0-9a-f]{64}$'),
    CHECK (profile->>'verified' = verified::text),
    CHECK (profile->'dkim_selectors' IS NOT NULL AND jsonb_typeof(profile->'dkim_selectors') = 'array')
);

CREATE INDEX sending_domain_profile_active ON sending_domain_profile (tenant_id, domain) WHERE active;

ALTER TABLE sending_domain_profile ENABLE ROW LEVEL SECURITY;
ALTER TABLE sending_domain_profile FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON sending_domain_profile
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON sending_domain_profile TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00335 is irreversible: active sending-domain ownership and verification state must be preserved'; END $$;
-- +goose StatementEnd
