-- HUB-026: section vectors per deployed version. Derived evidence rebuilt
-- from version bytes, carrying model, parser and content hashes with tenant
-- scope; retrieval constrains by current grants elsewhere.
-- +goose Up
CREATE TABLE document_section_vector (
    id text PRIMARY KEY, tenant_id text NOT NULL, document_id text NOT NULL, version_id text NOT NULL,
    block_id text NOT NULL, model_id text NOT NULL, model_version text NOT NULL,
    parser_version text NOT NULL, dim integer NOT NULL, vector bytea NOT NULL, content_hash text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, version_id, block_id, model_id),
    FOREIGN KEY (tenant_id, document_id) REFERENCES document(tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, version_id) REFERENCES document_version(tenant_id, id) ON DELETE CASCADE
);
CREATE INDEX document_section_vector_lookup ON document_section_vector(tenant_id, document_id, version_id, model_id);

-- +goose StatementBegin
DO $$ DECLARE t text; BEGIN
  FOREACH t IN ARRAY ARRAY['document_section_vector'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = current_setting(''hcmnext.tenant_id'', true)) WITH CHECK (tenant_id = current_setting(''hcmnext.tenant_id'', true))', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE document_section_vector;
