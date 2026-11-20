-- +goose Up
-- Issue 3791: the work a conversation proposed. One row says that a proposal
-- (an intent an installed agent put forward from this conversation) belongs to
-- the conversation. It holds no status and no content: what the proposal is
-- doing now is read from its owner under the reader's own authority, so this
-- table can never show a person something they may not open.
CREATE TABLE chat_work_link (
    tenant_id text NOT NULL,
    conversation_id text NOT NULL,
    kind text NOT NULL CHECK (kind IN ('INTENT_PROPOSAL')),
    source_id text NOT NULL CHECK (btrim(source_id) <> ''),
    proposed_by_id text NOT NULL,
    agent_installation_id text NOT NULL DEFAULT '',
    intent_type text NOT NULL DEFAULT '',
    linked_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, conversation_id, kind, source_id),
    FOREIGN KEY (tenant_id, conversation_id) REFERENCES chat_conversation(tenant_id, id) ON DELETE CASCADE
);
CREATE INDEX chat_work_link_conversation ON chat_work_link(tenant_id, conversation_id, linked_at DESC, source_id);
ALTER TABLE chat_work_link ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_work_link FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_work_link
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));
CREATE TRIGGER chat_work_link_immutable BEFORE UPDATE ON chat_work_link FOR EACH ROW EXECUTE FUNCTION chat_forbid_mutation();

-- +goose Down
DROP TABLE chat_work_link;
