-- HUB-023: stable heading-derived block anchors. Block rows are derived
-- evidence rebuilt from version bytes (like links), so redeploys never move
-- an anchor: the same heading slug appears once per version, and only an
-- explicit heading edit creates or retires an anchor id.
-- +goose Up
CREATE TABLE document_block (
    tenant_id text NOT NULL, document_id text NOT NULL, version_id text NOT NULL,
    block_id text NOT NULL, heading text NOT NULL,
    level integer NOT NULL, ordinal integer NOT NULL,
    PRIMARY KEY (tenant_id, version_id, block_id),
    FOREIGN KEY (tenant_id, document_id) REFERENCES document(tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, version_id) REFERENCES document_version(tenant_id, id) ON DELETE CASCADE
);
CREATE INDEX document_block_document ON document_block(tenant_id, document_id, block_id);

-- +goose StatementBegin
DO $$ DECLARE t text; BEGIN
  FOREACH t IN ARRAY ARRAY['document_block'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = current_setting(''hcmnext.tenant_id'', true)) WITH CHECK (tenant_id = current_setting(''hcmnext.tenant_id'', true))', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE document_block;
