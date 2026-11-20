-- +goose Up
-- Issue 3804: what an agent remembers in a channel. One row is a short fact a
-- person asked the channel's agents to keep, bound to the message it came from.
-- It starts PROPOSED; a channel manager approves it (and only then does any
-- agent read it) or anyone allowed may forget it. The words, the source, the
-- proposer, the audience (the channel's members), the classification and the
-- time to live never change after the row is written. Forgetting a row erases
-- its words. Whether the source message still stands is read from chat_post at
-- every use, so a deleted or edited source retires the memory without a job.
CREATE TABLE chat_channel_memory (
    tenant_id text NOT NULL,
    id text NOT NULL,
    conversation_id text NOT NULL,
    memory_text text NOT NULL DEFAULT '',
    source_post_id text NOT NULL,
    source_revision bigint NOT NULL CHECK (source_revision > 0),
    source_sequence bigint NOT NULL DEFAULT 0 CHECK (source_sequence >= 0),
    proposer_home_tenant_id text NOT NULL,
    proposer_id text NOT NULL,
    audience text NOT NULL DEFAULT 'CHANNEL_MEMBERS' CHECK (audience = 'CHANNEL_MEMBERS'),
    classification text NOT NULL DEFAULT 'INTERNAL' CHECK (classification <> ''),
    ttl_days integer NOT NULL DEFAULT 90 CHECK (ttl_days BETWEEN 1 AND 365),
    status text NOT NULL DEFAULT 'PROPOSED' CHECK (status IN ('PROPOSED','APPROVED','FORGOTTEN')),
    created_at timestamptz NOT NULL DEFAULT now(),
    approved_by_home_tenant_id text NOT NULL DEFAULT '',
    approved_by_id text NOT NULL DEFAULT '',
    approved_at timestamptz,
    expires_at timestamptz,
    forgotten_by_home_tenant_id text NOT NULL DEFAULT '',
    forgotten_by_id text NOT NULL DEFAULT '',
    forgotten_at timestamptz,
    PRIMARY KEY (tenant_id, id),
    FOREIGN KEY (tenant_id, conversation_id) REFERENCES chat_conversation(tenant_id, id) ON DELETE CASCADE,
    CHECK (status = 'FORGOTTEN' OR (btrim(memory_text) <> '' AND char_length(memory_text) <= 500)),
    CHECK (status <> 'APPROVED' OR (approved_at IS NOT NULL AND expires_at IS NOT NULL AND approved_by_id <> '')),
    CHECK (status <> 'FORGOTTEN' OR (memory_text = '' AND forgotten_at IS NOT NULL))
);
-- One live (not forgotten) memory per source message.
CREATE UNIQUE INDEX chat_channel_memory_live_source ON chat_channel_memory(tenant_id, conversation_id, source_post_id) WHERE status <> 'FORGOTTEN';
CREATE INDEX chat_channel_memory_conversation ON chat_channel_memory(tenant_id, conversation_id, created_at DESC, id) WHERE status <> 'FORGOTTEN';
ALTER TABLE chat_channel_memory ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_channel_memory FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_channel_memory
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));

-- +goose StatementBegin
CREATE FUNCTION chat_channel_memory_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF (OLD.tenant_id, OLD.id, OLD.conversation_id, OLD.source_post_id, OLD.source_revision, OLD.source_sequence, OLD.proposer_home_tenant_id, OLD.proposer_id, OLD.audience, OLD.classification, OLD.ttl_days, OLD.created_at)
       IS DISTINCT FROM (NEW.tenant_id, NEW.id, NEW.conversation_id, NEW.source_post_id, NEW.source_revision, NEW.source_sequence, NEW.proposer_home_tenant_id, NEW.proposer_id, NEW.audience, NEW.classification, NEW.ttl_days, NEW.created_at) THEN
        RAISE EXCEPTION 'a remembered item keeps its source, audience and time to live';
    END IF;
    IF OLD.status = 'FORGOTTEN' THEN
        RAISE EXCEPTION 'a forgotten item stays forgotten';
    END IF;
    IF NEW.status = 'PROPOSED' AND OLD.status <> 'PROPOSED' THEN
        RAISE EXCEPTION 'an item cannot go back to proposed';
    END IF;
    IF NEW.status <> 'FORGOTTEN' AND NEW.memory_text IS DISTINCT FROM OLD.memory_text THEN
        RAISE EXCEPTION 'the words of a remembered item are not edited';
    END IF;
    IF OLD.status = 'APPROVED' AND NEW.status = 'APPROVED' AND (NEW.approved_at, NEW.expires_at, NEW.approved_by_id) IS DISTINCT FROM (OLD.approved_at, OLD.expires_at, OLD.approved_by_id) THEN
        RAISE EXCEPTION 'an approval is not extended';
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER chat_channel_memory_guard BEFORE UPDATE ON chat_channel_memory FOR EACH ROW EXECUTE FUNCTION chat_channel_memory_guard();

-- +goose Down
DROP TABLE chat_channel_memory;
DROP FUNCTION chat_channel_memory_guard();
