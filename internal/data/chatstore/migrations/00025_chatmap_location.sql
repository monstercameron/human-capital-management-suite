-- +goose Up
-- Coordinates live only here, never in message snapshots or outbox payloads.
CREATE UNIQUE INDEX chatmap_post_scope ON chat_post(tenant_id,conversation_id,id);
CREATE TABLE chat_location_share (
 tenant_id text NOT NULL,
 conversation_id text NOT NULL,
 post_id text NOT NULL,
 id text NOT NULL,
 post_revision bigint NOT NULL CHECK (post_revision > 0),
 revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
 sharer_id text NOT NULL,
 sharer_tenant_id text NOT NULL,
	shared_at timestamptz NOT NULL DEFAULT now(),
	sharer_kind text NOT NULL DEFAULT 'human' CHECK(sharer_kind IN ('human','agent','integration','service')),
 place jsonb,
 expires_at timestamptz,
 ended boolean NOT NULL DEFAULT false,
 PRIMARY KEY (tenant_id,conversation_id,post_id,id),
 FOREIGN KEY (tenant_id,conversation_id,post_id) REFERENCES chat_post(tenant_id,conversation_id,id) ON DELETE CASCADE,
 FOREIGN KEY (tenant_id,conversation_id) REFERENCES chat_conversation(tenant_id,id) ON DELETE CASCADE,
 CHECK ((ended AND place IS NULL) OR (NOT ended AND place IS NOT NULL))
);
CREATE INDEX chat_location_expiry ON chat_location_share(tenant_id,expires_at) WHERE NOT ended;
ALTER TABLE chat_location_share ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_location_share FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_location_share
 USING (tenant_id=current_setting('hcmnext.tenant_id',true))
 WITH CHECK (tenant_id=current_setting('hcmnext.tenant_id',true));

-- Match grants only where a chat deployment role exists.
-- +goose StatementBegin
DO $$ DECLARE grant_row record; BEGIN
 FOR grant_row IN SELECT grantee,privilege_type FROM information_schema.role_table_grants
  WHERE table_schema=current_schema() AND table_name='chat_ephemeral_post' AND privilege_type IN ('SELECT','INSERT','UPDATE','DELETE')
 LOOP
  EXECUTE format('GRANT %s ON chat_location_share TO %I',grant_row.privilege_type,grant_row.grantee);
 END LOOP;
END $$;
-- +goose StatementEnd

-- Removing a reference ends its share. A held message preserves its location
-- in this same record, but scheduled expiry still erases it.
-- +goose StatementBegin
CREATE FUNCTION chatmap_post_location_lifecycle() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS (
  SELECT 1 FROM chat_record_inventory i JOIN chat_record_hold h ON h.tenant_id=i.tenant_id
   AND h.hold_id IN (SELECT jsonb_array_elements_text(i.hold_ids))
  WHERE i.tenant_id=NEW.tenant_id AND i.record_id='post:'||NEW.id AND h.released_at IS NULL
 ) THEN RETURN NEW; END IF;
 UPDATE chat_location_share SET place=NULL,ended=true,revision=revision+1
 WHERE tenant_id=NEW.tenant_id AND conversation_id=NEW.conversation_id AND post_id=NEW.id AND NOT ended
 AND (NEW.tombstoned OR NOT EXISTS (
  SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(NEW.references_json)='array' THEN NEW.references_json ELSE '[]'::jsonb END) r
   WHERE r->>'Kind'='LOCATION' AND r->>'ID'=chat_location_share.id
 ));
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER chatmap_post_location_lifecycle AFTER UPDATE OF tombstoned,references_json ON chat_post
 FOR EACH ROW EXECUTE FUNCTION chatmap_post_location_lifecycle();

-- +goose Down
DROP TRIGGER chatmap_post_location_lifecycle ON chat_post;
DROP FUNCTION chatmap_post_location_lifecycle();
DROP TABLE chat_location_share;
DROP INDEX chatmap_post_scope;
