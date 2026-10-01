-- Revisioned agent schedule/subscription controls and retained source deliveries.
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
CREATE TABLE agent_subscription_state (
 tenant_id uuid NOT NULL REFERENCES tenant(tenant_id), subscription_id text NOT NULL,
 revision bigint NOT NULL CHECK (revision > 0), payload jsonb NOT NULL,
 PRIMARY KEY (tenant_id,subscription_id)
);
CREATE TABLE agent_subscription_audit (
 tenant_id uuid NOT NULL, subscription_id text NOT NULL, revision bigint NOT NULL,
 payload jsonb NOT NULL, PRIMARY KEY (tenant_id,subscription_id,revision),
 FOREIGN KEY (tenant_id,subscription_id) REFERENCES agent_subscription_state(tenant_id,subscription_id)
);
CREATE TABLE agent_subscription_event (
 tenant_id uuid NOT NULL, source_key text NOT NULL, subscription_id text NOT NULL,
 admitted_at timestamptz NOT NULL, payload jsonb NOT NULL,
 PRIMARY KEY (tenant_id,source_key),
 FOREIGN KEY (tenant_id,subscription_id) REFERENCES agent_subscription_state(tenant_id,subscription_id)
);
CREATE TABLE agent_subscription_receipt (
 tenant_id uuid NOT NULL, source_key text NOT NULL, payload jsonb NOT NULL,
 PRIMARY KEY (tenant_id,source_key),
 FOREIGN KEY (tenant_id,source_key) REFERENCES agent_subscription_event(tenant_id,source_key)
);
-- +goose StatementBegin
DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['agent_schedule_state','agent_schedule_audit','agent_schedule_outbox','agent_schedule_receipt','agent_subscription_state','agent_subscription_audit','agent_subscription_event','agent_subscription_receipt'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',t);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',t);
  EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid)',t);
  EXECUTE format('GRANT SELECT,INSERT ON %I TO hcmnext_agent_app',t);
 END LOOP;
 FOREACH t IN ARRAY ARRAY['agent_schedule_audit','agent_schedule_outbox','agent_schedule_receipt','agent_subscription_audit','agent_subscription_event','agent_subscription_receipt'] LOOP
  EXECUTE format('CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION forbid_mutation()',t);
 END LOOP;
END $$;
-- +goose StatementEnd
GRANT UPDATE ON agent_schedule_state,agent_subscription_state TO hcmnext_agent_app;
CREATE INDEX agent_schedule_outbox_pending ON agent_schedule_outbox(tenant_id,enqueued_at,source_key);
CREATE INDEX agent_subscription_event_debounce ON agent_subscription_event(tenant_id,subscription_id,admitted_at DESC);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM agent_schedule_audit) OR EXISTS (SELECT 1 FROM agent_subscription_audit) THEN
  RAISE EXCEPTION 'cannot remove retained agent trigger owner history';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE agent_subscription_receipt,agent_subscription_event,agent_subscription_audit,agent_subscription_state,agent_schedule_receipt,agent_schedule_outbox,agent_schedule_audit,agent_schedule_state;
