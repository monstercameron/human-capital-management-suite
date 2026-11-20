-- +goose Up
-- Issue 3733: the chat notification feed. The feed itself (a mention, a reply
-- in a thread the person follows, a direct message) is computed from posts the
-- person can read at the time they ask, so a notification never outlives the
-- person's access to the message and never holds message text. This table
-- remembers only which of them the person has read, so the read state is the
-- same in every session. A row names a post and carries no text.
CREATE TABLE chat_notification_read (
    tenant_id text NOT NULL,
    home_tenant_id text NOT NULL,
    member_id text NOT NULL,
    post_id text NOT NULL,
    read_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, home_tenant_id, member_id, post_id)
);
ALTER TABLE chat_notification_read ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_notification_read FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_notification_read
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));

-- The store marks a notification read once and reads the marks. It never
-- changes or deletes one.
-- A serving role is given what the store runs against the table and nothing
-- more, whether its privileges came from the schema's defaults or from a grant
-- made before this table existed (migration 00041 has the history). Row level
-- security still scopes every statement to the caller's tenant.
-- +goose StatementBegin
DO $$ DECLARE role_name text; BEGIN
    FOR role_name IN SELECT DISTINCT grantee FROM information_schema.role_table_grants
      WHERE table_schema=current_schema() AND table_name='chat_post' AND privilege_type='UPDATE' AND grantee<>'PUBLIC'
    LOOP
      EXECUTE format('GRANT SELECT, INSERT ON chat_notification_read TO %I',role_name);
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE chat_notification_read;
