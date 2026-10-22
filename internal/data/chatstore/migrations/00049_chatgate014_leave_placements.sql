-- +goose Up
-- CHATGATE-014: leaving a conversation erases the labels and segments the gate
-- placed the person in, as it already erases their answers. The trigger of
-- migration 00023 runs in the transaction that ends the membership, whichever
-- path ends it; this adds the placements, which live in the gate state's
-- Segments.Assignments by person, to what it removes. The rest of the body is
-- unchanged.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION chatgate_leave_erasure() RETURNS trigger LANGUAGE plpgsql AS $$
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
 UPDATE chat_gate_state SET state_json=state_json #- ARRAY['Segments','Assignments',OLD.member_id]
  WHERE tenant_id=OLD.tenant_id AND conversation_id=OLD.conversation_id AND (state_json #> ARRAY['Segments','Assignments',OLD.member_id]) IS NOT NULL;
 IF TG_OP='DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION chatgate_leave_erasure() RETURNS trigger LANGUAGE plpgsql AS $$
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
