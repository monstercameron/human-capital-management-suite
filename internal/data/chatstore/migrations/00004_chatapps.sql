-- +goose Up
CREATE TABLE IF NOT EXISTS chat_app_installation (
 id text PRIMARY KEY, tenant_id text NOT NULL, conversation_id text NOT NULL,
 app_id text NOT NULL, version bigint NOT NULL, manifest jsonb NOT NULL,
 granted_scopes text[] NOT NULL, status text NOT NULL, approver text NOT NULL,
 revision bigint NOT NULL, created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL
);
CREATE TABLE IF NOT EXISTS chat_app_event (
 id text PRIMARY KEY, tenant_id text NOT NULL, conversation_id text NOT NULL, installation_id text NOT NULL,
 event_type text NOT NULL, sequence bigint NOT NULL, payload jsonb NOT NULL,
 occurred_at timestamptz NOT NULL, expires_at timestamptz, signature text NOT NULL
);
CREATE TABLE IF NOT EXISTS chat_app_event_seen (
 tenant_id text NOT NULL, event_id text NOT NULL, seen_at timestamptz NOT NULL,
 PRIMARY KEY (tenant_id,event_id)
);
CREATE INDEX IF NOT EXISTS chat_app_installation_scope ON chat_app_installation(tenant_id,conversation_id);
CREATE INDEX IF NOT EXISTS chat_app_event_replay ON chat_app_event(tenant_id,conversation_id,sequence);
ALTER TABLE chat_app_installation ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_app_installation FORCE ROW LEVEL SECURITY;
ALTER TABLE chat_app_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_app_event FORCE ROW LEVEL SECURITY;
ALTER TABLE chat_app_event_seen ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_app_event_seen FORCE ROW LEVEL SECURITY;
CREATE POLICY chat_app_installation_tenant ON chat_app_installation USING (tenant_id=current_setting('hcmnext.tenant_id',true)) WITH CHECK (tenant_id=current_setting('hcmnext.tenant_id',true));
CREATE POLICY chat_app_event_tenant ON chat_app_event USING (tenant_id=current_setting('hcmnext.tenant_id',true)) WITH CHECK (tenant_id=current_setting('hcmnext.tenant_id',true));
CREATE POLICY chat_app_event_seen_tenant ON chat_app_event_seen USING (tenant_id=current_setting('hcmnext.tenant_id',true)) WITH CHECK (tenant_id=current_setting('hcmnext.tenant_id',true));
