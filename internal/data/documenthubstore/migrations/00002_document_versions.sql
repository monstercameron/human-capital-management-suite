-- HUB-004: immutable Markdown version storage. The document row is the
-- stable identity; every byte of content lives in append-only
-- document_version rows with a deterministic content hash. Candidate,
-- review and deployment semantics arrive with the todos that own them.
-- +goose Up
CREATE TABLE document (
    id text PRIMARY KEY, tenant_id text NOT NULL, owner_id text NOT NULL,
    home text NOT NULL DEFAULT 'PERSONAL', lifecycle text NOT NULL DEFAULT 'DRAFT',
    classification_ceiling text NOT NULL DEFAULT 'INTERNAL',
    retention_series text NOT NULL DEFAULT 'knowledge-default',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id)
);
CREATE TABLE document_version (
    id text PRIMARY KEY, tenant_id text NOT NULL, document_id text NOT NULL,
    parent_id text NOT NULL DEFAULT '', creator_id text NOT NULL,
    title text NOT NULL DEFAULT '', locale text NOT NULL DEFAULT 'en-US',
    classification text NOT NULL DEFAULT 'INTERNAL',
    normalized_markdown text NOT NULL, content_hash text NOT NULL,
    renderer_profile text NOT NULL DEFAULT 'hub-v1', change_note text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, document_id) REFERENCES document(tenant_id, id) ON DELETE RESTRICT
);
CREATE INDEX document_version_document ON document_version(tenant_id, document_id, created_at);

CREATE TRIGGER document_version_immutable BEFORE UPDATE OR DELETE ON document_version FOR EACH ROW EXECUTE FUNCTION document_forbid_mutation();

-- +goose StatementBegin
DO $$ DECLARE t text; BEGIN
  FOREACH t IN ARRAY ARRAY['document','document_version'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = current_setting(''hcmnext.tenant_id'', true)) WITH CHECK (tenant_id = current_setting(''hcmnext.tenant_id'', true))', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE document_version, document CASCADE;
