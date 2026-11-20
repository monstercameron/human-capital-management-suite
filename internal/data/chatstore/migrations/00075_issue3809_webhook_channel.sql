-- +goose Up
-- Issue 3809: the public channels an administrator has allowed to publish
-- message events to signed outbound webhooks. A channel is not published unless
-- it has a row here, and only public channels may have one. The row says only
-- "this channel may be published"; which endpoint receives which fields is the
-- managed subscription's decision. The post that is delivered is read from
-- chat_post at delivery time, so an edited or deleted message is never sent
-- from a stale copy.
CREATE TABLE chat_webhook_channel (
    tenant_id text NOT NULL,
    conversation_id text NOT NULL,
    allowed_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, conversation_id),
    FOREIGN KEY (tenant_id, conversation_id) REFERENCES chat_conversation(tenant_id, id) ON DELETE CASCADE
);
ALTER TABLE chat_webhook_channel ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_webhook_channel FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_webhook_channel
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));

-- A serving role is given what the store runs against the table and nothing
-- more, whether its privileges came from the schema's defaults or from a grant
-- made before this table existed (migration 00041 has the history). Row level
-- security still scopes every statement to the caller's tenant.
-- +goose StatementBegin
DO $$ DECLARE role_name text; BEGIN
    FOR role_name IN SELECT DISTINCT grantee FROM information_schema.role_table_grants
      WHERE table_schema=current_schema() AND table_name='chat_post' AND privilege_type='UPDATE' AND grantee<>'PUBLIC'
    LOOP
      EXECUTE format('GRANT SELECT, INSERT, DELETE ON chat_webhook_channel TO %I',role_name);
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE chat_webhook_channel;
