-- HUB-019: canonical document links. One row per doc:-scheme reference in a
-- version's Markdown, rebuilt from source on every version creation, so the
-- table always mirrors current bytes. No foreign key on the target: links
-- may point at documents that do not exist yet, and HUB-020 validates them
-- before official deployment. Rebuilds delete and reinsert, so rows are not
-- append-only.
-- +goose Up
CREATE TABLE document_link (
    id text PRIMARY KEY, tenant_id text NOT NULL,
    source_document_id text NOT NULL, source_version_id text NOT NULL,
    label text NOT NULL DEFAULT '', target_document_id text NOT NULL DEFAULT '',
    pinned_version_id text NOT NULL DEFAULT '', block_id text NOT NULL DEFAULT '',
    state text NOT NULL DEFAULT 'malformed',
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (tenant_id, source_document_id) REFERENCES document(tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, source_version_id) REFERENCES document_version(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT document_link_state_check CHECK (state IN ('valid','malformed'))
);
CREATE INDEX document_link_source ON document_link(tenant_id, source_document_id, source_version_id);
CREATE INDEX document_link_target ON document_link(tenant_id, target_document_id);

-- +goose StatementBegin
DO $$ BEGIN
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', 'document_link');
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', 'document_link');
    EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = current_setting(''hcmnext.tenant_id'', true)) WITH CHECK (tenant_id = current_setting(''hcmnext.tenant_id'', true))', 'document_link');
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE document_link CASCADE;
