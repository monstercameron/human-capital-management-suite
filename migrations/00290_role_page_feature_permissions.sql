-- WEB-241: tenant-configurable CRUD grants for stable features nested within
-- a product page. Page permission remains the outer authority boundary; this
-- table can only narrow a page grant, never manufacture one.

-- +goose Up

CREATE TABLE role_page_feature_permission (
    tenant_id   tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    role_id     text NOT NULL,
    page_id     text NOT NULL CHECK (page_id ~ '^[a-z][a-z0-9-]{1,63}$'),
    feature_id  text NOT NULL CHECK (feature_id ~ '^[a-z][a-z0-9_]{1,62}$'),
    version     cas_version NOT NULL,
    can_view    boolean NOT NULL DEFAULT false,
    can_create  boolean NOT NULL DEFAULT false,
    can_update  boolean NOT NULL DEFAULT false,
    can_delete  boolean NOT NULL DEFAULT false,
    updated_by  text NOT NULL CHECK (updated_by <> ''),
    updated_at  timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (tenant_id, role_id, page_id, feature_id),
    FOREIGN KEY (tenant_id, role_id) REFERENCES access_role (tenant_id, role_id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, role_id, page_id) REFERENCES role_page_permission (tenant_id, role_id, page_id) ON DELETE CASCADE,
    CHECK (can_view OR NOT (can_create OR can_update OR can_delete))
);

ALTER TABLE role_page_feature_permission ENABLE ROW LEVEL SECURITY;
ALTER TABLE role_page_feature_permission FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON role_page_feature_permission
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE ON role_page_feature_permission TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00290 is irreversible: migrations 00279-00289 already broke the rollback chain, so this migration keeps that true rather than claiming a reversibility no rollback can ever reach'; END $$;
-- +goose StatementEnd
