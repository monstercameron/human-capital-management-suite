-- Owner: internal/data/contentregistrystore. REV-047-02: persist only accepted commercial
-- entitlement decisions with tenant-scoped publication and activation facts.
-- storage-disposition: industry_pack_publication, industry_pack_activation | tenant-scoped published product state | local PostgreSQL | tenant-local ACID | permanent.
-- +goose Up

CREATE TABLE industry_pack_publication (
    tenant_id uuid NOT NULL REFERENCES tenant (tenant_id),
    row_id uuid NOT NULL,
    pack_id text NOT NULL CHECK (pack_id <> ''),
    version integer NOT NULL CHECK (version > 0),
    industry text NOT NULL CHECK (industry <> ''),
    pack_digest text NOT NULL CHECK (pack_digest ~ '^sha256:[0-9a-f]{64}$'),
    entitlement_contract_id text NOT NULL CHECK (entitlement_contract_id <> ''),
    entitlement_revision bigint NOT NULL CHECK (entitlement_revision > 0),
    entitlement_fingerprint text NOT NULL CHECK (entitlement_fingerprint ~ '^[0-9a-f]{64}$'),
    report jsonb NOT NULL CHECK (jsonb_typeof(report) = 'object'),
    accepted_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, pack_id, version),
    CHECK (report->>'PackID' = pack_id),
    CHECK ((report->>'Version')::integer = version),
    CHECK (report->>'Digest' = pack_digest),
    CHECK (report->'Entitlement'->'decision'->>'Fingerprint' = entitlement_fingerprint),
    CHECK (report->'Entitlement'->'decision'->>'TenantID' = tenant_id::text)
);

ALTER TABLE industry_pack_publication ENABLE ROW LEVEL SECURITY;
ALTER TABLE industry_pack_publication FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON industry_pack_publication
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE TRIGGER industry_pack_publication_append_only BEFORE UPDATE OR DELETE ON industry_pack_publication FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
GRANT SELECT, INSERT ON industry_pack_publication TO hcmnext_app;

CREATE TABLE industry_pack_activation (
    tenant_id uuid NOT NULL REFERENCES tenant (tenant_id),
    row_id uuid NOT NULL,
    pack_id text NOT NULL CHECK (pack_id <> ''),
    version integer NOT NULL CHECK (version > 0),
    industry text NOT NULL CHECK (industry <> ''),
    target_cell text NOT NULL CHECK (target_cell <> ''),
    bundle_digest text NOT NULL CHECK (bundle_digest ~ '^sha256:[0-9a-f]{64}$'),
    receipt_digest text NOT NULL CHECK (receipt_digest ~ '^sha256:[0-9a-f]{64}$'),
    entitlement_contract_id text NOT NULL CHECK (entitlement_contract_id <> ''),
    entitlement_revision bigint NOT NULL CHECK (entitlement_revision > 0),
    entitlement_fingerprint text NOT NULL CHECK (entitlement_fingerprint ~ '^[0-9a-f]{64}$'),
    receipt jsonb NOT NULL CHECK (jsonb_typeof(receipt) = 'object'),
    composition jsonb,
    accepted_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, pack_id, version),
    CHECK (receipt->>'PackID' = pack_id),
    CHECK ((receipt->>'Version')::integer = version),
    CHECK (receipt->>'Industry' = industry),
    CHECK (receipt->>'BundleDigest' = bundle_digest),
    CHECK (receipt->>'Digest' = receipt_digest),
    CHECK (receipt->'Entitlement'->'decision'->>'Fingerprint' = entitlement_fingerprint),
    CHECK (receipt->'Entitlement'->'decision'->>'TenantID' = tenant_id::text),
    CHECK (composition IS NULL OR jsonb_typeof(composition) = 'object')
);

ALTER TABLE industry_pack_activation ENABLE ROW LEVEL SECURITY;
ALTER TABLE industry_pack_activation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON industry_pack_activation
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE TRIGGER industry_pack_activation_append_only BEFORE UPDATE OR DELETE ON industry_pack_activation FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
GRANT SELECT, INSERT ON industry_pack_activation TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00341 is irreversible: publication and activation evidence is append-only'; END $$;
-- +goose StatementEnd
