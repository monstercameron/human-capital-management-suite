-- Knowledge owns this migration tree. It is intentionally not part of /migrations.
-- HUB-001 lays the isolated document-database foundation: the publication
-- outbox only. Version, deployment, grant and link tables arrive with the
-- todos that own them (HUB-004 onwards).
-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION document_forbid_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'document append-only relation % cannot be changed', TG_TABLE_NAME; END $$;
-- +goose StatementEnd

CREATE TABLE document_outbox (
    id bigserial PRIMARY KEY, tenant_id text NOT NULL, aggregate_id text NOT NULL, event_type text NOT NULL,
    payload jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, aggregate_id, event_type, created_at)
);
CREATE TABLE document_outbox_receipt (
    tenant_id text NOT NULL, outbox_id bigint NOT NULL, published_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, outbox_id), FOREIGN KEY (outbox_id) REFERENCES document_outbox(id) ON DELETE CASCADE
);

CREATE INDEX document_outbox_pending ON document_outbox(tenant_id, id);

CREATE TRIGGER document_outbox_immutable BEFORE UPDATE OR DELETE ON document_outbox FOR EACH ROW EXECUTE FUNCTION document_forbid_mutation();
CREATE TRIGGER document_outbox_receipt_immutable BEFORE UPDATE OR DELETE ON document_outbox_receipt FOR EACH ROW EXECUTE FUNCTION document_forbid_mutation();

-- +goose StatementBegin
DO $$ DECLARE t text; BEGIN
  FOREACH t IN ARRAY ARRAY['document_outbox','document_outbox_receipt'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = current_setting(''hcmnext.tenant_id'', true)) WITH CHECK (tenant_id = current_setting(''hcmnext.tenant_id'', true))', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE document_outbox_receipt, document_outbox CASCADE;
DROP FUNCTION document_forbid_mutation();
