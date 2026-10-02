-- +goose NO TRANSACTION
-- +goose Up
-- These indexes do not change visibility. History keeps the existing sequence
-- keyset and continues returning tombstones. No duplicate text-search index.
-- Drop only this migration's index before each build so a cancelled concurrent
-- build's INVALID index cannot turn a resumed migration into a false success.
DROP INDEX CONCURRENTLY IF EXISTS chatscale_activity;
CREATE INDEX CONCURRENTLY chatscale_activity ON chat_post
 (tenant_id,conversation_id,created_at DESC,id DESC) WHERE NOT tombstoned;
DROP INDEX CONCURRENTLY IF EXISTS chatscale_members;
CREATE INDEX CONCURRENTLY chatscale_members ON chat_membership
 (tenant_id,home_tenant_id,member_id,conversation_id)
 INCLUDE (history_visibility,joined_at,revision) WHERE state='active';
DROP INDEX CONCURRENTLY IF EXISTS chatscale_member_count;
CREATE INDEX CONCURRENTLY chatscale_member_count ON chat_membership
 (tenant_id,conversation_id) WHERE state='active';
DROP INDEX CONCURRENTLY IF EXISTS chatscale_thread;
CREATE INDEX CONCURRENTLY chatscale_thread ON chat_post
 (tenant_id,conversation_id,parent_id,sequence);
DROP INDEX CONCURRENTLY IF EXISTS chatscale_reactions;
CREATE INDEX CONCURRENTLY chatscale_reactions ON chat_reaction
 (tenant_id,post_id,emoji,home_tenant_id,member_id) INCLUDE (created_at);
DROP INDEX CONCURRENTLY IF EXISTS chatscale_pins;
CREATE INDEX CONCURRENTLY chatscale_pins ON chat_pin
 (tenant_id,conversation_id,created_at DESC,post_id DESC)
 INCLUDE (home_tenant_id,member_id,revision);
-- Pins are removed by DELETE, not a state flag; a partial pin index would
-- exclude nothing. The post join continues excluding tombstoned messages.
DROP INDEX CONCURRENTLY IF EXISTS chatscale_unread;
CREATE INDEX CONCURRENTLY chatscale_unread ON chat_post
 (tenant_id,conversation_id,sequence DESC)
 INCLUDE (author_home_tenant_id,author_id,created_at) WHERE NOT tombstoned;
DROP INDEX CONCURRENTLY IF EXISTS chatscale_edits;
CREATE INDEX CONCURRENTLY chatscale_edits ON chat_post
 (tenant_id,conversation_id,updated_at,sequence DESC)
 INCLUDE (author_home_tenant_id,author_id,created_at) WHERE revision>1 AND NOT tombstoned;
-- Bodies and references deliberately stay out of btree INCLUDE payloads:
-- unconstrained reference lists can exceed PostgreSQL's index tuple limit.
DROP INDEX CONCURRENTLY IF EXISTS chatscale_outbox_scope;
CREATE INDEX CONCURRENTLY chatscale_outbox_scope ON chat_outbox
 (tenant_id,(payload->>'ConversationID'),id);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS chatscale_outbox_scope;
DROP INDEX CONCURRENTLY IF EXISTS chatscale_edits;
DROP INDEX CONCURRENTLY IF EXISTS chatscale_unread;
DROP INDEX CONCURRENTLY IF EXISTS chatscale_pins;
DROP INDEX CONCURRENTLY IF EXISTS chatscale_reactions;
DROP INDEX CONCURRENTLY IF EXISTS chatscale_thread;
DROP INDEX CONCURRENTLY IF EXISTS chatscale_member_count;
DROP INDEX CONCURRENTLY IF EXISTS chatscale_members;
DROP INDEX CONCURRENTLY IF EXISTS chatscale_activity;
