-- +goose Up
CREATE TABLE chat_gate_state (
 tenant_id text NOT NULL,
 conversation_id text NOT NULL,
 revision bigint NOT NULL DEFAULT 0 CHECK (revision >= 0),
 state_json jsonb NOT NULL,
 PRIMARY KEY (tenant_id,conversation_id),
 FOREIGN KEY (tenant_id,conversation_id) REFERENCES chat_conversation(tenant_id,id)
);
CREATE TABLE chat_gate_version (
 tenant_id text NOT NULL,
 conversation_id text NOT NULL,
 semantic_version text NOT NULL,
 digest text NOT NULL CHECK (digest ~ '^sha256:[a-f0-9]{64}$'),
 definition_json jsonb NOT NULL,
 PRIMARY KEY (tenant_id,conversation_id,semantic_version),
 FOREIGN KEY (tenant_id,conversation_id) REFERENCES chat_conversation(tenant_id,id)
);
CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON chat_gate_version FOR EACH ROW EXECUTE FUNCTION chat_forbid_mutation();
ALTER TABLE chat_gate_state ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_gate_state FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_gate_state USING (tenant_id=current_setting('hcmnext.tenant_id',true)) WITH CHECK (tenant_id=current_setting('hcmnext.tenant_id',true));
ALTER TABLE chat_gate_version ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_gate_version FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_gate_version USING (tenant_id=current_setting('hcmnext.tenant_id',true)) WITH CHECK (tenant_id=current_setting('hcmnext.tenant_id',true));

-- This fence covers direct add, invite and group lifecycle, including callers
-- which do not use the Go service. Gate publication and membership share a lock.
-- +goose StatementBegin
CREATE FUNCTION chatgate_membership_fence() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE data jsonb; version text;
BEGIN
 IF NEW.state <> 'active' THEN RETURN NEW; END IF;
 IF TG_OP='UPDATE' THEN
  IF OLD.state='active' AND (OLD.tenant_id,OLD.conversation_id,OLD.home_tenant_id,OLD.member_id)=(NEW.tenant_id,NEW.conversation_id,NEW.home_tenant_id,NEW.member_id) THEN RETURN NEW; END IF;
 END IF;
 PERFORM id FROM chat_conversation WHERE tenant_id=NEW.tenant_id AND id=NEW.conversation_id FOR UPDATE;
 SELECT state_json INTO data FROM chat_gate_state WHERE tenant_id=NEW.tenant_id AND conversation_id=NEW.conversation_id;
 IF data IS NULL OR coalesce(data->'Gate'->>'State','')<>'active' OR coalesce(data->'Gate'->>'Current','')='' THEN RETURN NEW; END IF;
 version:=data->'Gate'->>'Current';
 IF NEW.home_tenant_id<>NEW.tenant_id OR NOT (
  EXISTS (SELECT 1 FROM jsonb_array_elements(coalesce(nullif(data->'Submissions','null'::jsonb),'[]'::jsonb)) s WHERE s->>'Person'=NEW.member_id AND s->>'Status'='admitted' AND split_part(s->>'Version','.',1)=split_part(version,'.',1))
  OR EXISTS (SELECT 1 FROM jsonb_array_elements(coalesce(nullif(data->'Overrides','null'::jsonb),'[]'::jsonb)) o WHERE o->>'Person'=NEW.member_id AND o->>'Version'=version AND length(o->>'Reason')>0)
 ) THEN RAISE EXCEPTION 'channel gate answers required' USING ERRCODE='42501'; END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER chatgate_membership_fence BEFORE INSERT OR UPDATE ON chat_membership FOR EACH ROW EXECUTE FUNCTION chatgate_membership_fence();

-- +goose Down
DROP TRIGGER chatgate_membership_fence ON chat_membership;
DROP FUNCTION chatgate_membership_fence();
DROP TABLE chat_gate_version;
DROP TABLE chat_gate_state;
