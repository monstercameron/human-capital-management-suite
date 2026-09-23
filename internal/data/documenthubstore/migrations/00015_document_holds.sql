-- HUB-037: legal holds freezing disposal. Hold rows are mutable only
-- through release; disposal itself never touches the immutable core.
-- +goose Up
CREATE TABLE document_hold (
    id text PRIMARY KEY, tenant_id text NOT NULL, document_id text NOT NULL,
    reason text NOT NULL, placed_by text NOT NULL, active boolean NOT NULL DEFAULT true,
    placed_at timestamptz NOT NULL DEFAULT now(), released_at timestamptz,
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, document_id) REFERENCES document(tenant_id, id) ON DELETE CASCADE
);
CREATE INDEX document_hold_document ON document_hold(tenant_id, document_id, active);

-- +goose StatementBegin
DO $$ DECLARE t text; BEGIN
  FOREACH t IN ARRAY ARRAY['document_hold'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = current_setting(''hcmnext.tenant_id'', true)) WITH CHECK (tenant_id = current_setting(''hcmnext.tenant_id'', true))', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE document_hold;
