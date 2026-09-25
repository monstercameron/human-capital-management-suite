-- Harden the persisted event envelope consumed by bounded project outbox pulls.
-- Existing project mutations already write the complete versioned envelope in
-- the same transaction as activity and their canonical state change.
-- +goose Up
ALTER TABLE project_outbox ADD CONSTRAINT project_outbox_event_id_nonempty CHECK (length(btrim(event_id)) > 0);
ALTER TABLE project_outbox ADD CONSTRAINT project_outbox_event_type_nonempty CHECK (length(btrim(event_type)) > 0);
ALTER TABLE project_outbox ADD CONSTRAINT project_outbox_classification_nonempty CHECK (length(btrim(classification)) > 0);
ALTER TABLE project_outbox_cursor ADD CONSTRAINT project_outbox_consumer_nonempty CHECK (length(btrim(consumer)) > 0);

-- Reassert tenant isolation at the outbox boundary, including forced RLS for
-- table owners and administrative connections that are not BYPASSRLS.
-- +goose StatementBegin
DO $$ DECLARE t text; BEGIN
  FOREACH t IN ARRAY ARRAY['project_outbox','project_outbox_cursor'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('DROP POLICY IF EXISTS tenant_isolation ON %I', t);
    EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = current_setting(''hcmnext.tenant_id'', true)) WITH CHECK (tenant_id = current_setting(''hcmnext.tenant_id'', true))', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
ALTER TABLE project_outbox_cursor DROP CONSTRAINT project_outbox_consumer_nonempty;
ALTER TABLE project_outbox DROP CONSTRAINT project_outbox_classification_nonempty;
ALTER TABLE project_outbox DROP CONSTRAINT project_outbox_event_type_nonempty;
ALTER TABLE project_outbox DROP CONSTRAINT project_outbox_event_id_nonempty;
