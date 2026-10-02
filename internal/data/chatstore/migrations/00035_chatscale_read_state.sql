-- +goose Up
-- Constant defaults are metadata-only on supported PostgreSQL versions. Heads
-- are populated lazily or by BackfillChatHeads in bounded batches.
SET LOCAL lock_timeout = '2s';
ALTER TABLE chat_conversation ADD COLUMN chatscale_history_revision bigint NOT NULL DEFAULT 0;
ALTER TABLE chat_conversation ADD COLUMN chatscale_head_ready boolean NOT NULL DEFAULT false;
ALTER TABLE chat_conversation ADD COLUMN chatscale_last_activity_at timestamptz;
ALTER TABLE chat_conversation ADD COLUMN chatscale_last_author text;
ALTER TABLE chat_conversation ADD COLUMN chatscale_last_author_home text;

CREATE TABLE chatscale_read_state (
 tenant_id text NOT NULL, home_tenant_id text NOT NULL, member_id text NOT NULL,
 conversation_id text NOT NULL, history_revision bigint NOT NULL,
 member_revision bigint NOT NULL, history_visibility text NOT NULL, joined_at timestamptz NOT NULL,
 read_revision bigint NOT NULL, last_sequence bigint NOT NULL, read_at timestamptz NOT NULL,
 unread bigint NOT NULL CHECK(unread BETWEEN 0 AND 5000),
 mentions bigint NOT NULL CHECK(mentions BETWEEN 0 AND 5000),
 last_activity_at timestamptz,
 PRIMARY KEY(tenant_id,home_tenant_id,member_id,conversation_id),
 FOREIGN KEY(tenant_id,conversation_id) REFERENCES chat_conversation(tenant_id,id) ON DELETE CASCADE
);
ALTER TABLE chatscale_read_state ENABLE ROW LEVEL SECURITY;
ALTER TABLE chatscale_read_state FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chatscale_read_state
 USING(tenant_id=current_setting('hcmnext.tenant_id',true))
 WITH CHECK(tenant_id=current_setting('hcmnext.tenant_id',true));
-- +goose StatementBegin
DO $$ DECLARE role_name text; BEGIN
 FOR role_name IN SELECT DISTINCT grantee FROM information_schema.role_table_grants
 WHERE table_schema=current_schema() AND table_name='chat_post'
 AND privilege_type='SELECT' AND grantee<>'PUBLIC' LOOP
  EXECUTE format('GRANT SELECT,INSERT,UPDATE,DELETE ON chatscale_read_state TO %I',role_name);
 END LOOP;
END $$;
-- +goose StatementEnd

-- A generation is the invalidation fence, not a counter delta. It commits
-- with the history change, so a crash before an outbox consumer resumes cannot
-- serve an old badge. No fan-out per send: only affected conversation heads
-- are updated, once per SQL statement, even for a bulk removal.
-- +goose StatementBegin
CREATE FUNCTION chatscale_history_changed() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE changed text;
BEGIN
 IF TG_OP='INSERT' THEN changed:='SELECT DISTINCT tenant_id,conversation_id FROM new_posts';
 ELSIF TG_OP='DELETE' THEN changed:='SELECT DISTINCT tenant_id,conversation_id FROM old_posts';
 ELSE changed:='SELECT tenant_id,conversation_id FROM new_posts UNION SELECT tenant_id,conversation_id FROM old_posts';
 END IF;
 EXECUTE 'UPDATE chat_conversation c SET chatscale_history_revision=c.chatscale_history_revision+1,
 chatscale_head_ready=true,
 (chatscale_last_activity_at,chatscale_last_author,chatscale_last_author_home)=
 (SELECT p.created_at,p.author_id,p.author_home_tenant_id FROM chat_post p
 WHERE p.tenant_id=c.tenant_id AND p.conversation_id=c.id AND NOT p.tombstoned
 ORDER BY p.created_at DESC,p.id DESC LIMIT 1)
 FROM ('||changed||') changed WHERE c.tenant_id=changed.tenant_id AND c.id=changed.conversation_id';
 RETURN NULL;
END $$;
-- +goose StatementEnd
CREATE TRIGGER chatscale_insert AFTER INSERT ON chat_post
 REFERENCING NEW TABLE AS new_posts FOR EACH STATEMENT EXECUTE FUNCTION chatscale_history_changed();
CREATE TRIGGER chatscale_update AFTER UPDATE ON chat_post
 REFERENCING OLD TABLE AS old_posts NEW TABLE AS new_posts FOR EACH STATEMENT EXECUTE FUNCTION chatscale_history_changed();
CREATE TRIGGER chatscale_delete AFTER DELETE ON chat_post
 REFERENCING OLD TABLE AS old_posts FOR EACH STATEMENT EXECUTE FUNCTION chatscale_history_changed();

-- +goose Down
SET LOCAL lock_timeout = '2s';
DROP TRIGGER chatscale_delete ON chat_post;
DROP TRIGGER chatscale_update ON chat_post;
DROP TRIGGER chatscale_insert ON chat_post;
DROP FUNCTION chatscale_history_changed();
DROP TABLE chatscale_read_state;
ALTER TABLE chat_conversation DROP COLUMN chatscale_last_author_home;
ALTER TABLE chat_conversation DROP COLUMN chatscale_last_author;
ALTER TABLE chat_conversation DROP COLUMN chatscale_last_activity_at;
ALTER TABLE chat_conversation DROP COLUMN chatscale_head_ready;
ALTER TABLE chat_conversation DROP COLUMN chatscale_history_revision;
