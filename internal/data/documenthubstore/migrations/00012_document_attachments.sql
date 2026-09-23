-- HUB-024: safe attachment references. Rows are immutable evidence binding
-- an admitted quarantine artifact to the exact version hash and
-- classification under which it attached; reclassification revokes
-- usability without rewriting history. The row carries no object URL.
-- +goose Up
CREATE TABLE document_attachment (
    id text PRIMARY KEY, tenant_id text NOT NULL, document_id text NOT NULL, version_id text NOT NULL,
    version_hash text NOT NULL, artifact_id text NOT NULL,
    filename text NOT NULL, content_type text NOT NULL, size_bytes bigint NOT NULL,
    classification text NOT NULL, quarantine_state text NOT NULL, scanner_version text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, document_id) REFERENCES document(tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, version_id) REFERENCES document_version(tenant_id, id) ON DELETE CASCADE
);
CREATE INDEX document_attachment_version ON document_attachment(tenant_id, document_id, version_id);

CREATE TRIGGER document_attachment_immutable BEFORE UPDATE OR DELETE ON document_attachment FOR EACH ROW EXECUTE FUNCTION document_forbid_mutation();

-- +goose StatementBegin
DO $$ DECLARE t text; BEGIN
  FOREACH t IN ARRAY ARRAY['document_attachment'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = current_setting(''hcmnext.tenant_id'', true)) WITH CHECK (tenant_id = current_setting(''hcmnext.tenant_id'', true))', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE document_attachment;
