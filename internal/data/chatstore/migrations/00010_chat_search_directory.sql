-- +goose Up
CREATE INDEX chat_conversation_name_search ON chat_conversation USING gin (to_tsvector('simple', name)) WHERE lifecycle = 'ACTIVE';
CREATE INDEX chat_conversation_name_prefix ON chat_conversation (tenant_id, lower(name) text_pattern_ops, id) WHERE lifecycle = 'ACTIVE' AND kind IN ('PUBLIC_CHANNEL','PRIVATE_CHANNEL');

-- +goose Down
DROP INDEX IF EXISTS chat_conversation_name_search;
DROP INDEX IF EXISTS chat_conversation_name_prefix;
