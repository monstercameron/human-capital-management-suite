-- Governance metadata remains in the independently migrated chat database.
-- +goose Up
CREATE TABLE chat_audit_event (
 tenant_id text NOT NULL, event_id text NOT NULL, sequence bigint NOT NULL,
 actor_id text NOT NULL, action text NOT NULL, target_type text NOT NULL, target_id text NOT NULL,
 prior_revision bigint NOT NULL, reason text NOT NULL, policy_evidence text NOT NULL,
 at_time timestamptz NOT NULL, digest text NOT NULL, PRIMARY KEY (tenant_id,event_id), UNIQUE (tenant_id,sequence)
);
CREATE TABLE chat_record_inventory (tenant_id text NOT NULL, record_id text NOT NULL, conversation_id text NOT NULL, kind text NOT NULL, source_id text NOT NULL, revision bigint NOT NULL, created_at timestamptz NOT NULL, hold_ids jsonb NOT NULL DEFAULT '[]'::jsonb, disposition text NOT NULL DEFAULT '', derived_from text NOT NULL DEFAULT '', PRIMARY KEY (tenant_id,record_id));
CREATE TABLE chat_record_hold (tenant_id text NOT NULL, hold_id text NOT NULL, matter_ref text NOT NULL, reason text NOT NULL, placed_by text NOT NULL, placed_at timestamptz NOT NULL, released_at timestamptz, PRIMARY KEY (tenant_id,hold_id));
CREATE TABLE chat_record_export (tenant_id text NOT NULL, export_id text NOT NULL, record_ids jsonb NOT NULL, digest text NOT NULL, created_at timestamptz NOT NULL, PRIMARY KEY (tenant_id,export_id));
CREATE TABLE chat_moderation_report (tenant_id text NOT NULL, report_id text NOT NULL, conversation_id text NOT NULL, target_id text NOT NULL, reporter_id text NOT NULL, reason text NOT NULL, created_at timestamptz NOT NULL, state text NOT NULL, PRIMARY KEY (tenant_id,report_id));
CREATE TABLE chat_moderation_action (id bigserial PRIMARY KEY, tenant_id text NOT NULL, case_id text NOT NULL, action text NOT NULL, actor_id text NOT NULL, reason text NOT NULL, evidence_ref text NOT NULL, at_time timestamptz NOT NULL);
-- +goose StatementBegin
DO $$ DECLARE t text; BEGIN FOREACH t IN ARRAY ARRAY['chat_record_inventory','chat_audit_event','chat_record_hold','chat_record_export','chat_moderation_report','chat_moderation_action'] LOOP EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t); EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t); EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = current_setting(''hcmnext.tenant_id'', true)) WITH CHECK (tenant_id = current_setting(''hcmnext.tenant_id'', true))', t); END LOOP; END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE chat_moderation_action, chat_moderation_report, chat_record_export, chat_record_hold, chat_audit_event, chat_record_inventory;
