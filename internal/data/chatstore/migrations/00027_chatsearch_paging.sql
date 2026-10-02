-- +goose Up
-- Stream newest candidates through current row authority before matching.
CREATE INDEX chatsearch_post_page ON chat_post(tenant_id,created_at DESC,id COLLATE "C" DESC) WHERE NOT tombstoned;

-- +goose Down
DROP INDEX chatsearch_post_page;
