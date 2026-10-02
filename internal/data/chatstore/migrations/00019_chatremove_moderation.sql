-- +goose Up
ALTER TABLE chat_moderation_report ADD COLUMN reporter_home_tenant_id text NOT NULL DEFAULT '';
CREATE TABLE chat_admin_removal (
 tenant_id text NOT NULL, conversation_id text NOT NULL, post_id text NOT NULL,
 actor_id text NOT NULL, removed_at timestamptz NOT NULL, reason_code text NOT NULL,
 note text NOT NULL DEFAULT '', run_ref text NOT NULL DEFAULT '', restored_at timestamptz, restored_by text,
 appealed boolean NOT NULL DEFAULT false, queue_state text NOT NULL DEFAULT 'OPEN', revision bigint NOT NULL DEFAULT 1,
 PRIMARY KEY (tenant_id,conversation_id,post_id),
 FOREIGN KEY (tenant_id,conversation_id) REFERENCES chat_conversation(tenant_id,id),
 CHECK (reason_code IN ('harassment','sensitive_information','spam','policy_violation')),
 CHECK ((restored_at IS NULL) = (restored_by IS NULL))
);
CREATE TABLE chat_moderation_notice (
 tenant_id text NOT NULL, id text NOT NULL, conversation_id text NOT NULL, post_id text NOT NULL,
 home_tenant_id text NOT NULL, subject_id text NOT NULL, reason text NOT NULL,
 outcome text NOT NULL, at_time timestamptz NOT NULL,
 PRIMARY KEY (tenant_id,id,home_tenant_id,subject_id)
);
-- Current role projections are supplied by the trusted composition root, never
-- by request role claims. Channel roles come directly from chat_membership.
CREATE TABLE chat_moderation_role (
 tenant_id text NOT NULL, subject_id text NOT NULL, role text NOT NULL,
 revision bigint NOT NULL DEFAULT 1,
 PRIMARY KEY (tenant_id,subject_id,role)
);
CREATE TABLE chat_moderation_permission (
 tenant_id text NOT NULL, conversation_id text NOT NULL DEFAULT '', role text NOT NULL,
 permission text NOT NULL, allowed boolean NOT NULL, revision bigint NOT NULL DEFAULT 1,
 PRIMARY KEY (tenant_id,conversation_id,role,permission),
 CHECK (permission IN ('Report','Remove messages','Review removed messages','Manage filters'))
);
-- +goose StatementBegin
DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['chat_admin_removal','chat_moderation_notice','chat_moderation_role','chat_moderation_permission'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',t);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',t);
  EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id=current_setting(''hcmnext.tenant_id'',true)) WITH CHECK (tenant_id=current_setting(''hcmnext.tenant_id'',true))',t);
 END LOOP;
END $$;
-- +goose StatementEnd
CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON chat_moderation_notice FOR EACH ROW EXECUTE FUNCTION chat_forbid_mutation();
CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON chat_moderation_action FOR EACH ROW EXECUTE FUNCTION chat_forbid_mutation();
-- Grant insertions and revocations share the command's tenant fence, including
-- a newly inserted override that has no existing row to lock.
-- +goose StatementBegin
CREATE FUNCTION chatremove_permission_fence() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE host text;
BEGIN
 IF TG_OP='DELETE' THEN host:=OLD.tenant_id; ELSE host:=NEW.tenant_id; END IF;
 PERFORM pg_advisory_xact_lock(hashtextextended('chat-moderation:'||host,0));
 IF TG_OP='UPDATE' THEN NEW.revision:=OLD.revision+1; END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
END $$;
-- +goose StatementEnd
CREATE TRIGGER chatremove_permission_fence BEFORE INSERT OR UPDATE OR DELETE ON chat_moderation_permission FOR EACH ROW EXECUTE FUNCTION chatremove_permission_fence();
CREATE TRIGGER chatremove_role_fence BEFORE INSERT OR UPDATE OR DELETE ON chat_moderation_role FOR EACH ROW EXECUTE FUNCTION chatremove_permission_fence();
-- +goose Down
DROP TRIGGER forbid_mutation ON chat_moderation_action;
DROP TABLE chat_moderation_permission,chat_moderation_role,chat_moderation_notice,chat_admin_removal;
DROP FUNCTION chatremove_permission_fence();
ALTER TABLE chat_moderation_report DROP COLUMN reporter_home_tenant_id;
