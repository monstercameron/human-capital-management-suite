-- +goose Up
-- Issue 3792: a conversational decision recorded as its own object. A row is
-- written once; the only later change is that a newer decision replaces it
-- (superseded_by_id), so the history of a decision stays readable. Every
-- decision is informal: it records what a conversation settled and is never a
-- deployed policy.
CREATE TABLE chat_decision (
    tenant_id text NOT NULL,
    id text NOT NULL,
    conversation_id text NOT NULL,
    decision text NOT NULL CHECK (btrim(decision) <> '' AND char_length(decision) <= 500),
    rationale text NOT NULL DEFAULT '' CHECK (char_length(rationale) <= 2000),
    owner_home_tenant_id text NOT NULL,
    owner_id text NOT NULL,
    source_post_id text NOT NULL DEFAULT '',
    source_sequence bigint NOT NULL DEFAULT 0 CHECK (source_sequence >= 0),
    supersedes_id text NOT NULL DEFAULT '',
    superseded_by_id text NOT NULL DEFAULT '',
    informal boolean NOT NULL DEFAULT true CHECK (informal),
    decided_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    FOREIGN KEY (tenant_id, conversation_id) REFERENCES chat_conversation(tenant_id, id) ON DELETE CASCADE,
    CHECK (supersedes_id <> id AND superseded_by_id <> id)
);
CREATE INDEX chat_decision_conversation ON chat_decision(tenant_id, conversation_id, decided_at DESC, id);
ALTER TABLE chat_decision ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_decision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_decision
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));

-- +goose StatementBegin
CREATE FUNCTION chat_decision_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF (OLD.tenant_id, OLD.id, OLD.conversation_id, OLD.decision, OLD.rationale, OLD.owner_home_tenant_id, OLD.owner_id, OLD.source_post_id, OLD.source_sequence, OLD.supersedes_id, OLD.informal, OLD.decided_at)
       IS DISTINCT FROM (NEW.tenant_id, NEW.id, NEW.conversation_id, NEW.decision, NEW.rationale, NEW.owner_home_tenant_id, NEW.owner_id, NEW.source_post_id, NEW.source_sequence, NEW.supersedes_id, NEW.informal, NEW.decided_at) THEN
        RAISE EXCEPTION 'a recorded decision is not edited; record a new one that replaces it';
    END IF;
    IF OLD.superseded_by_id <> '' AND NEW.superseded_by_id IS DISTINCT FROM OLD.superseded_by_id THEN
        RAISE EXCEPTION 'a decision is replaced once';
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER chat_decision_guard BEFORE UPDATE ON chat_decision FOR EACH ROW EXECUTE FUNCTION chat_decision_guard();

-- +goose Down
DROP TABLE chat_decision;
DROP FUNCTION chat_decision_guard();
