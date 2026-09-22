-- +goose Up
CREATE TABLE chat_thread_follow (
    tenant_id text NOT NULL, home_tenant_id text NOT NULL, member_id text NOT NULL,
    conversation_id text NOT NULL, root_post_id text NOT NULL,
    followed boolean NOT NULL, revision bigint NOT NULL DEFAULT 1,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, home_tenant_id, member_id, conversation_id, root_post_id)
);
CREATE TABLE chat_personal_sidebar (
    tenant_id text NOT NULL, member_id text NOT NULL,
    layout jsonb NOT NULL DEFAULT '{}'::jsonb, revision bigint NOT NULL DEFAULT 1,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, member_id)
);
CREATE TABLE chat_quiet_hours (
    tenant_id text NOT NULL, member_id text NOT NULL,
    timezone text NOT NULL DEFAULT 'UTC', start_minute integer NOT NULL DEFAULT 1320,
    end_minute integer NOT NULL DEFAULT 420, enabled boolean NOT NULL DEFAULT false,
    revision bigint NOT NULL DEFAULT 1, updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, member_id),
    CHECK (start_minute BETWEEN 0 AND 1439), CHECK (end_minute BETWEEN 0 AND 1439)
);
-- +goose StatementBegin
DO $$ DECLARE t text; BEGIN
  FOREACH t IN ARRAY ARRAY['chat_thread_follow','chat_personal_sidebar','chat_quiet_hours'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = current_setting(''hcmnext.tenant_id'', true)) WITH CHECK (tenant_id = current_setting(''hcmnext.tenant_id'', true))', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE chat_quiet_hours, chat_personal_sidebar, chat_thread_follow;
