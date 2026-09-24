-- HUB-040: ownership transfer after departure. Each row is the immutable
-- fact that custody of a document moved from one owner to a successor;
-- history is never rewritten, mirroring document_deployment and
-- document_review. TransferOwnership (ownership.go) is the only writer.
-- +goose Up
CREATE TABLE document_ownership_transfer (
    id text PRIMARY KEY, tenant_id text NOT NULL, document_id text NOT NULL,
    prior_owner_id text NOT NULL, successor_owner_id text NOT NULL CHECK (successor_owner_id <> ''),
    reason text NOT NULL DEFAULT '', transferred_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, document_id) REFERENCES document(tenant_id, id) ON DELETE RESTRICT
);
CREATE INDEX document_ownership_transfer_document ON document_ownership_transfer(tenant_id, document_id, created_at DESC);

CREATE TRIGGER document_ownership_transfer_immutable BEFORE UPDATE OR DELETE ON document_ownership_transfer FOR EACH ROW EXECUTE FUNCTION document_forbid_mutation();

-- +goose StatementBegin
DO $$ DECLARE t text; BEGIN
  FOREACH t IN ARRAY ARRAY['document_ownership_transfer'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = current_setting(''hcmnext.tenant_id'', true)) WITH CHECK (tenant_id = current_setting(''hcmnext.tenant_id'', true))', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE document_ownership_transfer;
