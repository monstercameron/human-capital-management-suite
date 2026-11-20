-- +goose Up
-- Issue 3779: a legal hold can name one conversation. Until now a hold only
-- covered records that someone had tagged with its id, so a hold placed after
-- the fact protected nothing. conversation_id is the conversation the hold
-- covers ('' for a hold that names records directly, as before). released_by
-- and release_reason record who lifted the hold and why; the release itself
-- is also an audit event. The table keeps its tenant_isolation policy.
ALTER TABLE chat_record_hold ADD COLUMN conversation_id text NOT NULL DEFAULT '';
ALTER TABLE chat_record_hold ADD COLUMN released_by text NOT NULL DEFAULT '';
ALTER TABLE chat_record_hold ADD COLUMN release_reason text NOT NULL DEFAULT '';
CREATE INDEX chat_record_hold_conversation_idx ON chat_record_hold (tenant_id, conversation_id) WHERE conversation_id <> '' AND released_at IS NULL;

-- +goose Down
DROP INDEX chat_record_hold_conversation_idx;
ALTER TABLE chat_record_hold DROP COLUMN release_reason;
ALTER TABLE chat_record_hold DROP COLUMN released_by;
ALTER TABLE chat_record_hold DROP COLUMN conversation_id;
