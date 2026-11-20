-- +goose Up
-- Issue 3726: a journey that was shared into a conversation. The row reuses
-- chat_work_link: it says only that the journey (source_id) was put in this
-- conversation, by whom. Whether a viewer may see the conversation, and the
-- journey, is decided when the link is read, never from this table.
ALTER TABLE chat_work_link DROP CONSTRAINT chat_work_link_kind_check;
ALTER TABLE chat_work_link ADD CONSTRAINT chat_work_link_kind_check CHECK (kind IN ('INTENT_PROPOSAL','JOURNEY_SHARE'));
CREATE INDEX chat_work_link_source ON chat_work_link(tenant_id, kind, source_id, linked_at DESC);

-- +goose Down
DROP INDEX chat_work_link_source;
ALTER TABLE chat_work_link DROP CONSTRAINT chat_work_link_kind_check;
ALTER TABLE chat_work_link ADD CONSTRAINT chat_work_link_kind_check CHECK (kind IN ('INTENT_PROPOSAL'));
