-- +goose Up
-- Explicit admission authority for governed public channels. This population
-- lives in chat's database; core worker/role projections are not its fence.
CREATE TABLE chat_persona_channel_policy (
  tenant_id text NOT NULL,
  conversation_id text NOT NULL,
  revision bigint NOT NULL CHECK (revision > 0),
  policy_json jsonb NOT NULL,
  PRIMARY KEY (tenant_id,conversation_id),
  FOREIGN KEY (tenant_id,conversation_id) REFERENCES chat_conversation(tenant_id,id)
);
ALTER TABLE chat_persona_channel_policy ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_persona_channel_policy FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_persona_channel_policy USING (tenant_id=current_setting('hcmnext.tenant_id',true)) WITH CHECK (tenant_id=current_setting('hcmnext.tenant_id',true));
CREATE TABLE chat_public_audience_policy (
  tenant_id text NOT NULL,
  conversation_id text NOT NULL,
  revision bigint NOT NULL CHECK (revision > 0),
  classification text NOT NULL CHECK (classification IN ('PUBLIC','INTERNAL')),
  PRIMARY KEY (tenant_id,conversation_id),
  FOREIGN KEY (tenant_id,conversation_id) REFERENCES chat_conversation(tenant_id,id)
);
CREATE TABLE chat_public_audience_principal (
  tenant_id text NOT NULL,
  conversation_id text NOT NULL,
  home_tenant_id text NOT NULL CHECK (length(trim(home_tenant_id)) > 0),
  subject_id text NOT NULL CHECK (length(trim(subject_id)) > 0),
  guest boolean NOT NULL DEFAULT false,
  PRIMARY KEY (tenant_id,conversation_id,home_tenant_id,subject_id),
  FOREIGN KEY (tenant_id,conversation_id) REFERENCES chat_public_audience_policy(tenant_id,conversation_id)
);
ALTER TABLE chat_public_audience_policy ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_public_audience_policy FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_public_audience_policy USING (tenant_id=current_setting('hcmnext.tenant_id',true)) WITH CHECK (tenant_id=current_setting('hcmnext.tenant_id',true));
ALTER TABLE chat_public_audience_principal ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_public_audience_principal FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_public_audience_principal USING (tenant_id=current_setting('hcmnext.tenant_id',true)) WITH CHECK (tenant_id=current_setting('hcmnext.tenant_id',true));
CREATE TABLE chat_persona_source_classification (
  tenant_id text NOT NULL,
  conversation_id text NOT NULL,
  post_id text NOT NULL REFERENCES chat_post(id),
  body_digest text NOT NULL CHECK (body_digest ~ '^sha256:[a-f0-9]{64}$'),
  data_class text NOT NULL CHECK (data_class IN ('PUBLIC','INTERNAL','PII','COMPENSATION','BANK','MEDICAL','IMMIGRATION','CASE','SPECIAL_CATEGORY')),
  PRIMARY KEY (tenant_id,conversation_id,post_id),
  FOREIGN KEY (tenant_id,conversation_id) REFERENCES chat_conversation(tenant_id,id)
);
ALTER TABLE chat_persona_source_classification ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_persona_source_classification FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_persona_source_classification USING (tenant_id=current_setting('hcmnext.tenant_id',true)) WITH CHECK (tenant_id=current_setting('hcmnext.tenant_id',true));

-- Every write, including direct SQL, participates in the public commit fence.
-- +goose StatementBegin
CREATE FUNCTION chat_public_authority_fence() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE host text; room text;
BEGIN
  IF TG_OP = 'DELETE' THEN host := OLD.tenant_id; room := OLD.conversation_id;
  ELSE host := NEW.tenant_id; room := NEW.conversation_id; END IF;
  IF TG_OP = 'UPDATE' AND (OLD.tenant_id <> NEW.tenant_id OR OLD.conversation_id <> NEW.conversation_id) THEN
    RAISE EXCEPTION 'chat authority identity is immutable';
  END IF;
  IF TG_TABLE_NAME IN ('chat_membership','chat_channel_policy','chat_post')
     AND NOT EXISTS (SELECT 1 FROM chat_public_audience_policy WHERE tenant_id=host AND conversation_id=room)
     AND NOT EXISTS (SELECT 1 FROM chat_persona_channel_policy WHERE tenant_id=host AND conversation_id=room) THEN
    IF TG_OP = 'DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
  END IF;
  UPDATE chat_conversation SET audience_revision=audience_revision+1 WHERE tenant_id=host AND id=room;
  IF TG_TABLE_NAME = 'chat_membership' AND TG_OP <> 'DELETE' THEN
    IF NEW.state = 'active'
       AND EXISTS (SELECT 1 FROM chat_public_audience_policy WHERE tenant_id=host AND conversation_id=room)
       AND NOT EXISTS (SELECT 1 FROM chat_public_audience_principal WHERE tenant_id=host AND conversation_id=room AND home_tenant_id=NEW.home_tenant_id AND subject_id=NEW.member_id) THEN
      RAISE EXCEPTION 'member outside public channel admission population' USING ERRCODE='42501';
    END IF;
  END IF;
  IF TG_OP = 'DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
END $$;
-- +goose StatementEnd
CREATE TRIGGER chat_public_policy_fence BEFORE INSERT OR UPDATE OR DELETE ON chat_public_audience_policy FOR EACH ROW EXECUTE FUNCTION chat_public_authority_fence();
CREATE TRIGGER chat_persona_policy_fence BEFORE INSERT OR UPDATE OR DELETE ON chat_persona_channel_policy FOR EACH ROW EXECUTE FUNCTION chat_public_authority_fence();
CREATE TRIGGER chat_persona_source_class_fence BEFORE INSERT OR UPDATE OR DELETE ON chat_persona_source_classification FOR EACH ROW EXECUTE FUNCTION chat_public_authority_fence();
CREATE TRIGGER chat_public_principal_fence BEFORE INSERT OR UPDATE OR DELETE ON chat_public_audience_principal FOR EACH ROW EXECUTE FUNCTION chat_public_authority_fence();
CREATE TRIGGER chat_public_membership_fence BEFORE INSERT OR UPDATE OR DELETE ON chat_membership FOR EACH ROW EXECUTE FUNCTION chat_public_authority_fence();
CREATE TRIGGER chat_public_channel_policy_fence BEFORE INSERT OR UPDATE OR DELETE ON chat_channel_policy FOR EACH ROW EXECUTE FUNCTION chat_public_authority_fence();
-- Edits and removals of grounded sources invalidate in-flight disclosure checks.
CREATE TRIGGER chat_public_source_fence BEFORE UPDATE OR DELETE ON chat_post FOR EACH ROW EXECUTE FUNCTION chat_public_authority_fence();

-- +goose StatementBegin
CREATE FUNCTION chat_persona_conversation_fence() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF (OLD.kind,OLD.name,OLD.description,OLD.owner_id,OLD.lifecycle) IS DISTINCT FROM (NEW.kind,NEW.name,NEW.description,NEW.owner_id,NEW.lifecycle)
     AND (EXISTS (SELECT 1 FROM chat_public_audience_policy WHERE tenant_id=OLD.tenant_id AND conversation_id=OLD.id)
          OR EXISTS (SELECT 1 FROM chat_persona_channel_policy WHERE tenant_id=OLD.tenant_id AND conversation_id=OLD.id)) THEN
    NEW.audience_revision := greatest(NEW.audience_revision,OLD.audience_revision+1);
  END IF;
  RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER chat_persona_conversation_fence BEFORE UPDATE ON chat_conversation FOR EACH ROW EXECUTE FUNCTION chat_persona_conversation_fence();

-- +goose Down
DROP TRIGGER chat_persona_conversation_fence ON chat_conversation;
DROP FUNCTION chat_persona_conversation_fence();
DROP TRIGGER chat_public_source_fence ON chat_post;
DROP TRIGGER chat_public_channel_policy_fence ON chat_channel_policy;
DROP TRIGGER chat_public_membership_fence ON chat_membership;
DROP TRIGGER chat_public_principal_fence ON chat_public_audience_principal;
DROP TRIGGER chat_public_policy_fence ON chat_public_audience_policy;
DROP TRIGGER chat_persona_policy_fence ON chat_persona_channel_policy;
DROP TRIGGER chat_persona_source_class_fence ON chat_persona_source_classification;
DROP FUNCTION chat_public_authority_fence();
DROP TABLE chat_public_audience_principal;
DROP TABLE chat_public_audience_policy;
DROP TABLE chat_persona_channel_policy;
DROP TABLE chat_persona_source_classification;
