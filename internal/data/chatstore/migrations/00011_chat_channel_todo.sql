-- +goose Up
CREATE TABLE chat_channel_todo (
    tenant_id text NOT NULL,
    conversation_id text NOT NULL,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    pinned boolean NOT NULL DEFAULT false,
    items_json jsonb NOT NULL DEFAULT '[]'::jsonb,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, conversation_id),
    FOREIGN KEY (tenant_id, conversation_id) REFERENCES chat_conversation(tenant_id, id) ON DELETE CASCADE,
    CHECK (jsonb_typeof(items_json) = 'array')
);
ALTER TABLE chat_channel_todo ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_channel_todo FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_channel_todo
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));

CREATE TABLE chat_channel_todo_revision (
    tenant_id text NOT NULL,
    conversation_id text NOT NULL,
    revision bigint NOT NULL CHECK (revision > 1),
    actor_home_tenant_id text NOT NULL,
    actor_id text NOT NULL,
    operation text NOT NULL,
    prior_pinned boolean NOT NULL,
    pinned boolean NOT NULL,
    prior_items_json jsonb NOT NULL,
    items_json jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, conversation_id, revision),
    FOREIGN KEY (tenant_id, conversation_id) REFERENCES chat_conversation(tenant_id, id) ON DELETE CASCADE
);
CREATE TRIGGER chat_channel_todo_revision_immutable BEFORE UPDATE OR DELETE ON chat_channel_todo_revision
    FOR EACH ROW EXECUTE FUNCTION chat_forbid_mutation();
ALTER TABLE chat_channel_todo_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_channel_todo_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_channel_todo_revision
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));

-- +goose Down
DROP TABLE chat_channel_todo_revision;
DROP TABLE chat_channel_todo;
