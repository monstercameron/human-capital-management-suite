-- +goose NO TRANSACTION
-- +goose Up
-- CHATSCALE-003: a plain send no longer invalidates every member's cached
-- badge. A cached row records the conversation head it was counted at
-- (head_sequence) and how many posts its counting window held (window_rows);
-- the sidebar read adds only the posts after the head. Edits, removals and
-- deletions still move chatscale_history_revision, so the bounded scan stays
-- the rebuild path and the test oracle.
--
-- Every statement is idempotent, so the migration can be run again after a crash.
SET lock_timeout = '2s';
-- Rows written before this migration have neither column. A row without both is
-- never served and is overwritten by the first sidebar read of its owner, which
-- counts without reading post bodies; stamping them here would need a recount of
-- history (the window size is not recoverable from the row), and the table
-- forces row-level security, so a migration role could not reach the rows anyway.
ALTER TABLE chatscale_read_state ADD COLUMN IF NOT EXISTS head_sequence bigint;
ALTER TABLE chatscale_read_state ADD COLUMN IF NOT EXISTS window_rows bigint;

-- Insert keeps the conversation head (last activity and author) current and
-- keeps post_sequence at or above every stored sequence, which makes the head
-- stamp trustworthy for rows written outside the send path. It does not touch
-- the history revision. Update and delete keep chatscale_history_changed.
--
-- A single post that is newer than the recorded head becomes the head directly;
-- that is the plain send, and it skips the lookup of the latest post (the send
-- paid 0.8 ms in this trigger before, 0.45 ms after). Anything else (a batch, an
-- older or tied timestamp, a removed post, a head not yet computed) takes the
-- general path, which recomputes the head from history exactly as before.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION chatscale_head_appended() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE one record; n int;
BEGIN
 SELECT count(*) INTO n FROM new_posts;
 IF n=1 THEN
  SELECT tenant_id,conversation_id,sequence,created_at,author_id,author_home_tenant_id,tombstoned INTO one FROM new_posts;
  UPDATE chat_conversation c SET post_sequence=GREATEST(c.post_sequence,one.sequence),
   chatscale_last_activity_at=one.created_at,chatscale_last_author=one.author_id,chatscale_last_author_home=one.author_home_tenant_id
  WHERE c.tenant_id=one.tenant_id AND c.id=one.conversation_id AND c.chatscale_head_ready AND NOT one.tombstoned
   AND one.created_at>c.chatscale_last_activity_at;
  IF FOUND THEN RETURN NULL; END IF;
 END IF;
 UPDATE chat_conversation c SET chatscale_head_ready=true,
  post_sequence=GREATEST(c.post_sequence,changed.max_sequence),
  (chatscale_last_activity_at,chatscale_last_author,chatscale_last_author_home)=
  (SELECT p.created_at,p.author_id,p.author_home_tenant_id FROM chat_post p
   WHERE p.tenant_id=c.tenant_id AND p.conversation_id=c.id AND NOT p.tombstoned
   ORDER BY p.created_at DESC,p.id DESC LIMIT 1)
 FROM (SELECT tenant_id,conversation_id,max(sequence) AS max_sequence FROM new_posts GROUP BY tenant_id,conversation_id) changed
 WHERE c.tenant_id=changed.tenant_id AND c.id=changed.conversation_id;
 RETURN NULL;
END $$;
-- +goose StatementEnd
DROP TRIGGER IF EXISTS chatscale_insert ON chat_post;
CREATE TRIGGER chatscale_insert AFTER INSERT ON chat_post
 REFERENCING NEW TABLE AS new_posts FOR EACH STATEMENT EXECUTE FUNCTION chatscale_head_appended();
RESET lock_timeout;

-- A first sidebar read counts without reading post bodies: eligible posts are
-- counted from chatscale_unread alone, and the two kinds of post the count must
-- correct for are found through their own small partial indexes (the "added
-- people" line, and posts that mention anybody).
DROP INDEX CONCURRENTLY IF EXISTS chatscale_system_lines;
CREATE INDEX CONCURRENTLY chatscale_system_lines ON chat_post
 (tenant_id,conversation_id,sequence DESC) INCLUDE (author_home_tenant_id,author_id,created_at)
 WHERE NOT tombstoned AND body LIKE chr(8291)||'member-added:%';
DROP INDEX CONCURRENTLY IF EXISTS chatscale_mention_posts;
CREATE INDEX CONCURRENTLY chatscale_mention_posts ON chat_post
 (tenant_id,conversation_id,sequence DESC) INCLUDE (author_home_tenant_id,author_id,created_at)
 WHERE NOT tombstoned AND references_json @> '[{"Kind":"PERSON_MENTION"}]'::jsonb;

-- CHATSCALE-002: chat_post_history (tenant_id, conversation_id, sequence DESC) is
-- the unique key (tenant_id, conversation_id, sequence) read backwards: 115 MB
-- of index at a million posts and a write on every send, for no plan the key
-- cannot serve (every history read and the "open small channel" read measured
-- the same without it). chat_post_touched stays: it is not covered by
-- chatscale_edits (which holds only edited posts), and the moderation queue's
-- author-and-time lookup goes from 0.07 ms to 350 ms at a million posts without it.
DROP INDEX CONCURRENTLY IF EXISTS chat_post_history;

-- +goose Down
SET lock_timeout = '2s';
CREATE INDEX CONCURRENTLY IF NOT EXISTS chat_post_history ON chat_post(tenant_id, conversation_id, sequence DESC);
DROP INDEX CONCURRENTLY IF EXISTS chatscale_mention_posts;
DROP INDEX CONCURRENTLY IF EXISTS chatscale_system_lines;
DROP TRIGGER IF EXISTS chatscale_insert ON chat_post;
CREATE TRIGGER chatscale_insert AFTER INSERT ON chat_post
 REFERENCING NEW TABLE AS new_posts FOR EACH STATEMENT EXECUTE FUNCTION chatscale_history_changed();
DROP FUNCTION IF EXISTS chatscale_head_appended();
-- Sends bumped no revision while the delta was live, so no cached row can be trusted.
TRUNCATE chatscale_read_state;
ALTER TABLE chatscale_read_state DROP COLUMN IF EXISTS window_rows;
ALTER TABLE chatscale_read_state DROP COLUMN IF EXISTS head_sequence;
RESET lock_timeout;
