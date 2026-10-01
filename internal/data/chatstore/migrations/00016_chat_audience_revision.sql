-- +goose Up
-- The audience fence advances only when membership changes. Post and outbox
-- activity must not invalidate an audience snapshot by accident.
ALTER TABLE chat_conversation ADD COLUMN audience_revision bigint NOT NULL DEFAULT 1;

-- +goose Down
ALTER TABLE chat_conversation DROP COLUMN audience_revision;
