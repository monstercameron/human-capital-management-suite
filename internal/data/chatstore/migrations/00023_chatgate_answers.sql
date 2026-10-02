-- +goose Up
-- Values are deliberately separate from immutable definitions and metadata.
CREATE TABLE chat_gate_submission_revision (
 tenant_id text NOT NULL,
 conversation_id text NOT NULL,
 submission_id text NOT NULL,
 revision bigint NOT NULL,
 record_json jsonb NOT NULL,
 PRIMARY KEY (tenant_id,conversation_id,submission_id,revision),
 FOREIGN KEY (tenant_id,conversation_id) REFERENCES chat_conversation(tenant_id,id)
);
CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON chat_gate_submission_revision FOR EACH ROW EXECUTE FUNCTION chat_forbid_mutation();
CREATE TABLE chat_gate_answer (
 tenant_id text NOT NULL,
 conversation_id text NOT NULL,
 submission_id text NOT NULL,
 person_id text NOT NULL,
 field_id text NOT NULL,
 value_json jsonb NOT NULL,
 expires_at timestamptz NOT NULL,
 legal_hold boolean NOT NULL DEFAULT false,
 PRIMARY KEY (tenant_id,conversation_id,submission_id,field_id),
 FOREIGN KEY (tenant_id,conversation_id) REFERENCES chat_conversation(tenant_id,id)
);
CREATE INDEX chat_gate_answer_retention ON chat_gate_answer(tenant_id,expires_at);
CREATE TABLE chat_gate_read_audit (
 tenant_id text NOT NULL,
 conversation_id text NOT NULL,
 sequence bigint NOT NULL,
 audit_json jsonb NOT NULL,
 PRIMARY KEY (tenant_id,conversation_id,sequence),
 FOREIGN KEY (tenant_id,conversation_id) REFERENCES chat_conversation(tenant_id,id)
);
CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON chat_gate_read_audit FOR EACH ROW EXECUTE FUNCTION chat_forbid_mutation();
CREATE TABLE chat_gate_membership_basis (
 tenant_id text NOT NULL,
 conversation_id text NOT NULL,
 person_id text NOT NULL,
 semantic_version text NOT NULL,
 revision bigint NOT NULL,
 active boolean NOT NULL,
 PRIMARY KEY (tenant_id,conversation_id,person_id,revision),
 FOREIGN KEY (tenant_id,conversation_id) REFERENCES chat_conversation(tenant_id,id)
);
CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON chat_gate_membership_basis FOR EACH ROW EXECUTE FUNCTION chat_forbid_mutation();
-- +goose StatementBegin
DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['chat_gate_answer','chat_gate_read_audit','chat_gate_membership_basis','chat_gate_submission_revision'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',t);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',t);
  EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id=current_setting(''hcmnext.tenant_id'',true)) WITH CHECK (tenant_id=current_setting(''hcmnext.tenant_id'',true))',t);
 END LOOP;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION chatgate_leave_erasure() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' THEN
  IF NEW.state='active' OR OLD.state<>'active' THEN RETURN NEW; END IF;
 END IF;
 DELETE FROM chat_gate_answer WHERE tenant_id=OLD.tenant_id AND conversation_id=OLD.conversation_id AND person_id=OLD.member_id AND NOT legal_hold;
 UPDATE chat_gate_state SET revision=revision+1,state_json=jsonb_set(
  jsonb_set(state_json,'{Gate,Revision}',to_jsonb(revision+1)),
  '{Submissions}',coalesce((SELECT jsonb_agg(CASE WHEN s->>'Person'=OLD.member_id THEN jsonb_set(jsonb_set(s,'{Status}','"withdrawn"'::jsonb),'{Revision}',to_jsonb(coalesce((s->>'Revision')::bigint,0)+1)) ELSE s END) FROM jsonb_array_elements(coalesce(nullif(state_json->'Submissions','null'::jsonb),'[]'::jsonb)) s),'[]'::jsonb)
 ) WHERE tenant_id=OLD.tenant_id AND conversation_id=OLD.conversation_id;
 UPDATE chat_gate_state SET state_json=jsonb_set(state_json,'{Overrides}',coalesce((SELECT jsonb_agg(o) FROM jsonb_array_elements(coalesce(nullif(state_json->'Overrides','null'::jsonb),'[]'::jsonb)) o WHERE o->>'Person'<>OLD.member_id),'[]'::jsonb)) WHERE tenant_id=OLD.tenant_id AND conversation_id=OLD.conversation_id;
 IF TG_OP='DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
END $$;
-- +goose StatementEnd
CREATE TRIGGER chatgate_leave_erasure AFTER UPDATE OR DELETE ON chat_membership FOR EACH ROW EXECUTE FUNCTION chatgate_leave_erasure();

-- +goose Down
DROP TRIGGER chatgate_leave_erasure ON chat_membership;
DROP FUNCTION chatgate_leave_erasure();
DROP TABLE chat_gate_submission_revision;
DROP TABLE chat_gate_membership_basis;
DROP TABLE chat_gate_read_audit;
DROP TABLE chat_gate_answer;
