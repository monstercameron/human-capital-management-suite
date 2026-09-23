-- +goose Up
CREATE TABLE chat_channel_poll (
    tenant_id text NOT NULL,
    conversation_id text NOT NULL,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    question text NOT NULL DEFAULT '',
    options_json jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(options_json) = 'array'),
    votes_json jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(votes_json) = 'array'),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, conversation_id),
    FOREIGN KEY (tenant_id, conversation_id) REFERENCES chat_conversation(tenant_id, id) ON DELETE CASCADE
);
ALTER TABLE chat_channel_poll ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_channel_poll FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_channel_poll
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));

CREATE TABLE chat_channel_poll_revision (
    tenant_id text NOT NULL,
    conversation_id text NOT NULL,
    revision bigint NOT NULL CHECK (revision > 1),
    actor_home_tenant_id text NOT NULL,
    actor_id text NOT NULL,
    operation text NOT NULL CHECK (operation IN ('CREATE', 'VOTE')),
    prior_question text NOT NULL,
    question text NOT NULL,
    prior_options_json jsonb NOT NULL,
    options_json jsonb NOT NULL,
    prior_votes_json jsonb NOT NULL,
    votes_json jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, conversation_id, revision),
    FOREIGN KEY (tenant_id, conversation_id) REFERENCES chat_conversation(tenant_id, id) ON DELETE CASCADE
);
CREATE TRIGGER chat_channel_poll_revision_immutable BEFORE UPDATE OR DELETE ON chat_channel_poll_revision
    FOR EACH ROW EXECUTE FUNCTION chat_forbid_mutation();
ALTER TABLE chat_channel_poll_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_channel_poll_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_channel_poll_revision
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));

-- +goose Down
DROP TABLE chat_channel_poll_revision;
DROP TABLE chat_channel_poll;
