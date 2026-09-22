-- +goose Up
CREATE TABLE chat_channel_policy (
  tenant_id text NOT NULL,
  conversation_id text NOT NULL,
  revision bigint NOT NULL CHECK (revision > 0),
  required_roles text[] NOT NULL DEFAULT '{}',
  role_mode integer NOT NULL CHECK (role_mode IN (1,2)),
  required_qualifications text[] NOT NULL DEFAULT '{}',
  allowed_principals text[] NOT NULL DEFAULT '{}',
  allowed_tenants text[] NOT NULL DEFAULT '{}',
  classification text NOT NULL DEFAULT '',
  residency text NOT NULL DEFAULT '',
  PRIMARY KEY (tenant_id,conversation_id),
  FOREIGN KEY (tenant_id,conversation_id) REFERENCES chat_conversation(tenant_id,id)
);

CREATE TABLE chat_share_grant (
  id text PRIMARY KEY,
  tenant_id text NOT NULL,
  conversation_id text NOT NULL,
  consumer_tenant text NOT NULL CHECK (consumer_tenant <> tenant_id),
  version bigint NOT NULL CHECK (version > 0),
  scope text NOT NULL CHECK (scope = 'conversation'),
  classification text NOT NULL,
  residency text NOT NULL,
  proposed_by text NOT NULL,
  proposed_at timestamptz NOT NULL DEFAULT now(),
  accepted_by text,
  accepted_at timestamptz,
  expires_at timestamptz NOT NULL,
  revoked_by text,
  revoked_at timestamptz,
  FOREIGN KEY (tenant_id,conversation_id) REFERENCES chat_conversation(tenant_id,id),
  CHECK ((accepted_by IS NULL) = (accepted_at IS NULL)),
  CHECK ((revoked_by IS NULL) = (revoked_at IS NULL))
);
CREATE INDEX chat_share_grant_current ON chat_share_grant (tenant_id,consumer_tenant,conversation_id,expires_at) WHERE accepted_at IS NOT NULL AND revoked_at IS NULL;
CREATE UNIQUE INDEX chat_share_grant_one_unrevoked ON chat_share_grant (tenant_id,consumer_tenant,conversation_id) WHERE revoked_at IS NULL;

ALTER TABLE chat_channel_policy ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_channel_policy FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_channel_policy USING (tenant_id=current_setting('hcmnext.tenant_id',true)) WITH CHECK (tenant_id=current_setting('hcmnext.tenant_id',true));
ALTER TABLE chat_share_grant ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_share_grant FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_share_grant USING (tenant_id=current_setting('hcmnext.tenant_id',true)) WITH CHECK (tenant_id=current_setting('hcmnext.tenant_id',true));

-- +goose Down
DROP TABLE chat_share_grant;
DROP TABLE chat_channel_policy;
