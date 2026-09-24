-- I18N-004: immutable tenant/product translation revisions and activation history.
-- storage-disposition: activated_translation_catalog_revision, activated_translation_catalog_event | reviewed localized product copy | local PostgreSQL | tenant-local ACID publication | append-only.
--
-- +goose Up

CREATE TABLE activated_translation_catalog_revision (
    tenant_id uuid NOT NULL REFERENCES tenant (tenant_id),
    product_id text NOT NULL CHECK (product_id <> ''),
    locale text NOT NULL CHECK (locale <> ''),
    revision_id text NOT NULL CHECK (revision_id <> ''),
    digest char(64) NOT NULL CHECK (digest ~ '^[0-9a-f]{64}$'),
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    PRIMARY KEY (tenant_id, product_id, locale, revision_id),
    UNIQUE (tenant_id, product_id, locale, revision_id, digest),
    CHECK (payload->>'id' = revision_id),
    CHECK (payload->>'locale' = locale),
    CHECK (payload->>'canonical_digest' = digest::text)
);

CREATE TABLE activated_translation_catalog_event (
    tenant_id uuid NOT NULL,
    product_id text NOT NULL,
    locale text NOT NULL,
    sequence bigserial NOT NULL,
    revision_id text NOT NULL,
    digest char(64) NOT NULL,
    actor_id text NOT NULL CHECK (actor_id <> ''),
    activated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, product_id, locale, sequence),
    FOREIGN KEY (tenant_id, product_id, locale, revision_id, digest)
        REFERENCES activated_translation_catalog_revision (tenant_id, product_id, locale, revision_id, digest)
);

CREATE INDEX activated_translation_catalog_event_latest
    ON activated_translation_catalog_event (tenant_id, product_id, locale, sequence DESC);

ALTER TABLE activated_translation_catalog_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE activated_translation_catalog_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON activated_translation_catalog_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE TRIGGER activated_translation_catalog_revision_append_only
    BEFORE UPDATE OR DELETE ON activated_translation_catalog_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
GRANT SELECT, INSERT ON activated_translation_catalog_revision TO hcmnext_app;

ALTER TABLE activated_translation_catalog_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE activated_translation_catalog_event FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON activated_translation_catalog_event
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE TRIGGER activated_translation_catalog_event_append_only
    BEFORE UPDATE OR DELETE ON activated_translation_catalog_event
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
GRANT SELECT, INSERT ON activated_translation_catalog_event TO hcmnext_app;
GRANT USAGE, SELECT ON SEQUENCE activated_translation_catalog_event_sequence_seq TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00350 is irreversible: activated catalog revisions and events are publication evidence'; END $$;
-- +goose StatementEnd
