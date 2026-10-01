-- AGENT-031: Scheduling owns publications, cursor/outbox and acknowledgements
-- in the source database; accepted agent admissions and executions are separate.
-- +goose Up
CREATE TABLE agent_schedule_state (
 tenant_id uuid NOT NULL REFERENCES tenant(tenant_id), schedule_id text NOT NULL,
 revision bigint NOT NULL CHECK (revision > 0), payload jsonb NOT NULL,
 PRIMARY KEY (tenant_id,schedule_id)
);
CREATE TABLE agent_schedule_audit (
 tenant_id uuid NOT NULL, schedule_id text NOT NULL, revision bigint NOT NULL,
 actor_id text NOT NULL, action text NOT NULL, occurred_at timestamptz NOT NULL, payload jsonb NOT NULL,
 PRIMARY KEY (tenant_id,schedule_id,revision),
 FOREIGN KEY (tenant_id,schedule_id) REFERENCES agent_schedule_state(tenant_id,schedule_id)
);
CREATE TABLE agent_schedule_outbox (
 tenant_id uuid NOT NULL, source_key text NOT NULL, schedule_id text NOT NULL,
 request_digest text NOT NULL, enqueued_at timestamptz NOT NULL, payload jsonb NOT NULL,
 PRIMARY KEY (tenant_id,source_key),
 FOREIGN KEY (tenant_id,schedule_id) REFERENCES agent_schedule_state(tenant_id,schedule_id)
);
CREATE TABLE agent_schedule_receipt (
 tenant_id uuid NOT NULL, source_key text NOT NULL, payload jsonb NOT NULL,
 PRIMARY KEY (tenant_id,source_key),
 FOREIGN KEY (tenant_id,source_key) REFERENCES agent_schedule_outbox(tenant_id,source_key)
);
-- +goose StatementBegin
DO $$ DECLARE name text; BEGIN
 FOREACH name IN ARRAY ARRAY['agent_schedule_state','agent_schedule_audit','agent_schedule_outbox','agent_schedule_receipt'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',name);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',name);
  EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid)',name);
  EXECUTE format('GRANT SELECT,INSERT ON %I TO hcmnext_app',name);
 END LOOP;
 FOREACH name IN ARRAY ARRAY['agent_schedule_audit','agent_schedule_outbox','agent_schedule_receipt'] LOOP
  EXECUTE format('CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION forbid_mutation()',name);
 END LOOP;
END $$;
-- +goose StatementEnd
GRANT UPDATE ON agent_schedule_state TO hcmnext_app;
CREATE INDEX agent_schedule_outbox_pending ON agent_schedule_outbox(tenant_id,enqueued_at,source_key);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM agent_schedule_audit) THEN
  RAISE EXCEPTION 'cannot remove retained schedule source owner history';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE agent_schedule_receipt,agent_schedule_outbox,agent_schedule_audit,agent_schedule_state;
