-- UXAUDIT-022: retain tenant-owned brand asset bytes and immutable appearance revisions.

-- +goose Up

CREATE TABLE brand_asset_revision (
    tenant_id       uuid        NOT NULL REFERENCES tenant(tenant_id),
    revision        bigint      NOT NULL CHECK (revision > 0),
    asset_digest    text        NOT NULL CHECK (asset_digest ~ '^[0-9a-f]{64}$'),
    filename        text        NOT NULL CHECK (filename <> '' AND char_length(filename) <= 120),
    media_type      text        NOT NULL CHECK (media_type IN ('image/png', 'image/jpeg', 'image/webp')),
    width           integer     NOT NULL CHECK (width BETWEEN 16 AND 1024),
    height          integer     NOT NULL CHECK (height BETWEEN 16 AND 1024),
    original_bytes  bytea       NOT NULL CHECK (octet_length(original_bytes) BETWEEN 1 AND 2097152),
    proxy_bytes     bytea       NOT NULL CHECK (octet_length(proxy_bytes) BETWEEN 1 AND 2097152),
    proxy_type      text        NOT NULL CHECK (proxy_type = 'image/jpeg'),
    removed         boolean     NOT NULL DEFAULT false,
    actor_id        text        NOT NULL CHECK (btrim(actor_id) <> ''),
    created_at      timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, revision)
);

CREATE TABLE brand_asset_head (
    tenant_id uuid   NOT NULL REFERENCES tenant(tenant_id),
    revision  bigint NOT NULL,
    PRIMARY KEY (tenant_id),
    FOREIGN KEY (tenant_id, revision) REFERENCES brand_asset_revision(tenant_id, revision)
);

CREATE INDEX brand_asset_revision_history ON brand_asset_revision (tenant_id, revision DESC);

ALTER TABLE brand_asset_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE brand_asset_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON brand_asset_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE TRIGGER brand_asset_revision_immutable
    BEFORE UPDATE OR DELETE ON brand_asset_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE brand_asset_head ENABLE ROW LEVEL SECURITY;
ALTER TABLE brand_asset_head FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON brand_asset_head
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE, DELETE ON brand_asset_revision, brand_asset_head TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM brand_asset_revision) THEN
        RAISE EXCEPTION 'cannot remove retained brand asset revisions';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE brand_asset_head;
DROP TABLE brand_asset_revision;
