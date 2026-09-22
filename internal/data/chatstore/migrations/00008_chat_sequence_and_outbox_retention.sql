-- +goose Up

-- Per-conversation post sequence counter.
--
-- The send path derived the next sequence from COALESCE(max(sequence),0)+1 over
-- the surviving rows of chat_post. Retention purges and legal-hold deletions
-- remove rows, so that expression reissued sequence numbers that cursors,
-- idempotency records and audit evidence had already used. The counter now
-- lives on the conversation row, is advanced under the same row lock that the
-- route fence takes, and never moves backwards when posts are deleted.
ALTER TABLE chat_conversation ADD COLUMN post_sequence bigint NOT NULL DEFAULT 0;
UPDATE chat_conversation c
   SET post_sequence = COALESCE((SELECT max(p.sequence) FROM chat_post p WHERE p.tenant_id = c.tenant_id AND p.conversation_id = c.id), 0);

-- Bounded outbox retention.
--
-- 00001 installed BEFORE UPDATE OR DELETE forbid triggers on chat_outbox and
-- chat_outbox_receipt, which made the outbox unprunable: every event a chat
-- database ever emitted had to stay forever. Chat event volume is the reason
-- this database is separate at all, so unbounded growth is not acceptable.
--
-- The simplest permitted form is chosen here: keep the trigger but narrow it to
-- BEFORE UPDATE. Rows remain immutable once written - no producer or consumer
-- can rewrite history - while a retention job may DELETE events that are both
-- past their retention horizon and already consumed. A SECURITY DEFINER escape
-- hatch was rejected because it would run the pruner outside row level security
-- and needs a second privilege boundary to audit for no extra guarantee.
--
-- chatstore.PruneOutbox is the only supported pruner: it deletes only rows that
-- carry a publish receipt, are older than the caller's horizon and sit at or
-- below the slowest registered consumer cursor, in bounded batches.
DROP TRIGGER chat_outbox_immutable ON chat_outbox;
DROP TRIGGER chat_outbox_receipt_immutable ON chat_outbox_receipt;
CREATE TRIGGER chat_outbox_immutable BEFORE UPDATE ON chat_outbox FOR EACH ROW EXECUTE FUNCTION chat_forbid_mutation();
CREATE TRIGGER chat_outbox_receipt_immutable BEFORE UPDATE ON chat_outbox_receipt FOR EACH ROW EXECUTE FUNCTION chat_forbid_mutation();

-- Consumer cursors. A named consumer records how far it has drained so that
-- catch-up does not rescan the whole tenant from id 0 and so that retention can
-- tell which events are still owed to somebody.
CREATE TABLE chat_outbox_cursor (
    tenant_id text NOT NULL,
    consumer text NOT NULL,
    last_outbox_id bigint NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, consumer)
);
ALTER TABLE chat_outbox_cursor ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_outbox_cursor FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_outbox_cursor USING (tenant_id = current_setting('hcmnext.tenant_id', true)) WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));

CREATE INDEX chat_outbox_retention ON chat_outbox(tenant_id, created_at);

-- Recipient unread and mention counting scanned every post in a conversation
-- and expanded references_json per row. The GIN index serves the containment
-- predicate the counter now uses, and the updated_at index serves the edited
-- branch of the same query.
CREATE INDEX chat_post_references ON chat_post USING gin (references_json);
CREATE INDEX chat_post_touched ON chat_post(tenant_id, conversation_id, updated_at);

-- +goose Down
DROP INDEX chat_post_touched;
DROP INDEX chat_post_references;
DROP INDEX chat_outbox_retention;
DROP TABLE chat_outbox_cursor;
DROP TRIGGER chat_outbox_receipt_immutable ON chat_outbox_receipt;
DROP TRIGGER chat_outbox_immutable ON chat_outbox;
CREATE TRIGGER chat_outbox_immutable BEFORE UPDATE OR DELETE ON chat_outbox FOR EACH ROW EXECUTE FUNCTION chat_forbid_mutation();
CREATE TRIGGER chat_outbox_receipt_immutable BEFORE UPDATE OR DELETE ON chat_outbox_receipt FOR EACH ROW EXECUTE FUNCTION chat_forbid_mutation();
ALTER TABLE chat_conversation DROP COLUMN post_sequence;
