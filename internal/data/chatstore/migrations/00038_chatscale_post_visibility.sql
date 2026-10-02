-- +goose NO TRANSACTION
-- +goose Up
-- Pin listing needs only a live post's identity. The post primary key forces a
-- heap visit (and competes with large message payloads) for every pin. This
-- partial identity index lets that visibility join use an index-only lookup.
DROP INDEX CONCURRENTLY IF EXISTS chatscale_post_visibility;
CREATE INDEX CONCURRENTLY chatscale_post_visibility ON chat_post(tenant_id,conversation_id,id) WHERE NOT tombstoned;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS chatscale_post_visibility;
