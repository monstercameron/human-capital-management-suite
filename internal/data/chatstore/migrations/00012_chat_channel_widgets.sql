-- +goose Up
CREATE TABLE chat_channel_widget (
    tenant_id text NOT NULL,
    conversation_id text NOT NULL,
    kind text NOT NULL CHECK (kind IN ('TEAM', 'PROJECT')),
    revision bigint NOT NULL CHECK (revision > 1),
    pinned boolean NOT NULL DEFAULT false,
    payload_json jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(payload_json) = 'object'),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, conversation_id, kind),
    FOREIGN KEY (tenant_id, conversation_id) REFERENCES chat_conversation(tenant_id, id) ON DELETE CASCADE
);
ALTER TABLE chat_channel_widget ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_channel_widget FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_channel_widget
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));

CREATE TABLE chat_channel_widget_revision (
    tenant_id text NOT NULL,
    conversation_id text NOT NULL,
    kind text NOT NULL CHECK (kind IN ('TEAM', 'PROJECT')),
    revision bigint NOT NULL CHECK (revision > 1),
    actor_home_tenant_id text NOT NULL,
    actor_id text NOT NULL,
    operation text NOT NULL,
    prior_pinned boolean NOT NULL,
    pinned boolean NOT NULL,
    prior_payload_json jsonb NOT NULL,
    payload_json jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, conversation_id, kind, revision),
    FOREIGN KEY (tenant_id, conversation_id) REFERENCES chat_conversation(tenant_id, id) ON DELETE CASCADE
);
CREATE TRIGGER chat_channel_widget_revision_immutable BEFORE UPDATE OR DELETE ON chat_channel_widget_revision
    FOR EACH ROW EXECUTE FUNCTION chat_forbid_mutation();
ALTER TABLE chat_channel_widget_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_channel_widget_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_channel_widget_revision
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));

-- +goose Down
DROP TABLE chat_channel_widget_revision;
DROP TABLE chat_channel_widget;
