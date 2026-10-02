-- +goose Up
ALTER TABLE chat_conversation
  ADD COLUMN status_changed_by text NOT NULL DEFAULT '',
  ADD COLUMN status_changed_at timestamptz,
  ADD COLUMN status_reason text NOT NULL DEFAULT '',
  ADD COLUMN status_until timestamptz,
  ADD CONSTRAINT chatstate_until_lock CHECK (status_until IS NULL OR lifecycle='LOCKED');

CREATE TABLE chatstate_permission (
  tenant_id text NOT NULL,
  conversation_id text NOT NULL DEFAULT '',
  role_id text NOT NULL,
  change_open boolean NOT NULL DEFAULT false,
  change_restricted boolean NOT NULL DEFAULT false,
  post_announcements boolean NOT NULL DEFAULT false,
  revision bigint NOT NULL CHECK (revision>0),
  PRIMARY KEY (tenant_id,conversation_id,role_id)
);
ALTER TABLE chatstate_permission ENABLE ROW LEVEL SECURITY;
ALTER TABLE chatstate_permission FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chatstate_permission USING (tenant_id=current_setting('hcmnext.tenant_id',true)) WITH CHECK (tenant_id=current_setting('hcmnext.tenant_id',true));
-- +goose StatementBegin
DO $$ DECLARE role_name text; BEGIN
  FOR role_name IN SELECT grantee FROM information_schema.role_table_grants
    WHERE table_schema=current_schema() AND table_name='chat_conversation' AND privilege_type='SELECT'
  LOOP
    EXECUTE format('GRANT SELECT,INSERT,UPDATE ON chatstate_permission TO %I',role_name);
  END LOOP;
END $$;
-- +goose StatementEnd

-- Serialize content mutations with status changes, including writes that raced
-- the service's authorization read. Expired locks cease to fence immediately.
-- +goose StatementBegin
CREATE FUNCTION chatstate_content_fence() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE host text; room text; life text; until_time timestamptz; room_kind text;
BEGIN
  IF TG_OP='DELETE' THEN host:=OLD.tenant_id; ELSE host:=NEW.tenant_id; END IF;
  IF TG_TABLE_NAME='chat_reaction' THEN
    IF TG_OP='DELETE' THEN
      SELECT conversation_id INTO room FROM chat_post WHERE tenant_id=host AND id=OLD.post_id;
    ELSE
      SELECT conversation_id INTO room FROM chat_post WHERE tenant_id=host AND id=NEW.post_id;
    END IF;
  ELSE
    IF TG_OP='DELETE' THEN room:=OLD.conversation_id; ELSE room:=NEW.conversation_id; END IF;
  END IF;
  SELECT lifecycle,status_until,kind INTO life,until_time,room_kind FROM chat_conversation WHERE tenant_id=host AND id=room FOR UPDATE;
  IF room_kind NOT IN ('PUBLIC_CHANNEL','PRIVATE_CHANNEL') THEN
    IF TG_OP='DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
  END IF;
  IF life='ARCHIVED' OR (life='LOCKED' AND (until_time IS NULL OR until_time>statement_timestamp())) THEN
    RAISE EXCEPTION 'channel status forbids content mutation' USING ERRCODE='42501';
  END IF;
  IF TG_OP='DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
END $$;
-- +goose StatementEnd
CREATE TRIGGER chatstate_post_fence BEFORE INSERT OR UPDATE OR DELETE ON chat_post FOR EACH ROW EXECUTE FUNCTION chatstate_content_fence();
CREATE TRIGGER chatstate_reaction_fence BEFORE INSERT OR UPDATE OR DELETE ON chat_reaction FOR EACH ROW EXECUTE FUNCTION chatstate_content_fence();
CREATE TRIGGER chatstate_pin_fence BEFORE INSERT OR UPDATE OR DELETE ON chat_pin FOR EACH ROW EXECUTE FUNCTION chatstate_content_fence();

-- +goose StatementBegin
CREATE FUNCTION chatstate_membership_fence() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE host text; room text; life text; room_kind text;
BEGIN
  IF TG_OP='DELETE' THEN host:=OLD.tenant_id; room:=OLD.conversation_id;
  ELSE host:=NEW.tenant_id; room:=NEW.conversation_id; END IF;
  SELECT lifecycle,kind INTO life,room_kind FROM chat_conversation WHERE tenant_id=host AND id=room FOR UPDATE;
  IF room_kind IN ('PUBLIC_CHANNEL','PRIVATE_CHANNEL') AND life='ARCHIVED' THEN RAISE EXCEPTION 'archived channel forbids membership changes' USING ERRCODE='42501'; END IF;
  IF TG_OP='DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
END $$;
-- +goose StatementEnd
CREATE TRIGGER chatstate_membership_fence BEFORE INSERT OR UPDATE OR DELETE ON chat_membership FOR EACH ROW EXECUTE FUNCTION chatstate_membership_fence();

-- +goose StatementBegin
CREATE FUNCTION chatstate_metadata_fence() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.kind IN ('PUBLIC_CHANNEL','PRIVATE_CHANNEL') AND OLD.lifecycle='ARCHIVED' AND (OLD.name,OLD.description,OLD.owner_id) IS DISTINCT FROM (NEW.name,NEW.description,NEW.owner_id) THEN
    RAISE EXCEPTION 'archived channel forbids metadata changes' USING ERRCODE='42501';
  END IF;
  RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER chatstate_metadata_fence BEFORE UPDATE ON chat_conversation FOR EACH ROW EXECUTE FUNCTION chatstate_metadata_fence();

-- +goose StatementBegin
CREATE FUNCTION chatstate_hold_fence() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.kind IN ('PUBLIC_CHANNEL','PRIVATE_CHANNEL') AND (TG_OP='DELETE' OR (NEW.lifecycle='ARCHIVED' AND OLD.lifecycle<>'ARCHIVED')) AND
     ((OLD.lifecycle='LOCKED' AND (OLD.status_until IS NULL OR OLD.status_until>statement_timestamp())) OR EXISTS (
       SELECT 1 FROM chat_record_inventory i JOIN chat_record_hold h ON h.tenant_id=i.tenant_id
       AND h.hold_id IN (SELECT jsonb_array_elements_text(i.hold_ids))
       WHERE i.tenant_id=OLD.tenant_id AND (i.conversation_id=OLD.id OR i.record_id='conversation:'||OLD.id) AND h.released_at IS NULL
     )) THEN
    RAISE EXCEPTION 'held or locked channel cannot be archived or deleted' USING ERRCODE='42501';
  END IF;
  IF TG_OP='DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
END $$;
-- +goose StatementEnd
CREATE TRIGGER chatstate_hold_fence BEFORE UPDATE OR DELETE ON chat_conversation FOR EACH ROW EXECUTE FUNCTION chatstate_hold_fence();

-- +goose Down
DROP TABLE chatstate_permission;
DROP TRIGGER chatstate_metadata_fence ON chat_conversation;
DROP FUNCTION chatstate_metadata_fence();
DROP TRIGGER chatstate_membership_fence ON chat_membership;
DROP FUNCTION chatstate_membership_fence();
DROP TRIGGER chatstate_hold_fence ON chat_conversation;
DROP FUNCTION chatstate_hold_fence();
DROP TRIGGER chatstate_pin_fence ON chat_pin;
DROP TRIGGER chatstate_reaction_fence ON chat_reaction;
DROP TRIGGER chatstate_post_fence ON chat_post;
DROP FUNCTION chatstate_content_fence();
ALTER TABLE chat_conversation DROP CONSTRAINT chatstate_until_lock,
  DROP COLUMN status_until, DROP COLUMN status_reason,
  DROP COLUMN status_changed_at, DROP COLUMN status_changed_by;
