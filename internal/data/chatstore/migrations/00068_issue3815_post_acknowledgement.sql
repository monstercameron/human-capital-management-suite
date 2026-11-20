-- +goose Up
-- Issue 3815: a post that asks every member of its conversation to confirm
-- they have read and understood it. The requirement is written once by the
-- person who sent the post; each person's acknowledgement is written once by
-- that person. Both are kept as evidence, so neither can be changed or removed.
-- A read is never an acknowledgement: only the Acknowledge action writes here.
CREATE TABLE chat_ack_requirement (
    tenant_id text NOT NULL,
    conversation_id text NOT NULL,
    post_id text NOT NULL,
    required_by_id text NOT NULL CHECK (btrim(required_by_id) <> ''),
    post_revision bigint NOT NULL DEFAULT 0,
    required_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, conversation_id, post_id),
    FOREIGN KEY (tenant_id, conversation_id) REFERENCES chat_conversation(tenant_id, id) ON DELETE CASCADE
);
CREATE TABLE chat_ack_record (
    tenant_id text NOT NULL,
    conversation_id text NOT NULL,
    post_id text NOT NULL,
    subject_id text NOT NULL CHECK (btrim(subject_id) <> ''),
    post_revision bigint NOT NULL DEFAULT 0,
    acknowledged_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, conversation_id, post_id, subject_id),
    FOREIGN KEY (tenant_id, conversation_id, post_id) REFERENCES chat_ack_requirement(tenant_id, conversation_id, post_id) ON DELETE CASCADE
);
CREATE INDEX chat_ack_record_subject ON chat_ack_record(tenant_id, subject_id, acknowledged_at DESC);
ALTER TABLE chat_ack_requirement ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_ack_requirement FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_ack_requirement
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));
ALTER TABLE chat_ack_record ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_ack_record FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_ack_record
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));
CREATE TRIGGER chat_ack_requirement_immutable BEFORE UPDATE ON chat_ack_requirement FOR EACH ROW EXECUTE FUNCTION chat_forbid_mutation();
CREATE TRIGGER chat_ack_record_immutable BEFORE UPDATE ON chat_ack_record FOR EACH ROW EXECUTE FUNCTION chat_forbid_mutation();

-- The store writes a requirement and an acknowledgement once each and reads
-- them. It never changes or deletes either.
-- A serving role is given what the store runs against the table and nothing
-- more, whether its privileges came from the schema's defaults or from a grant
-- made before this table existed (migration 00041 has the history). Row level
-- security still scopes every statement to the caller's tenant.
-- +goose StatementBegin
DO $$ DECLARE role_name text; BEGIN
    FOR role_name IN SELECT DISTINCT grantee FROM information_schema.role_table_grants
      WHERE table_schema=current_schema() AND table_name='chat_post' AND privilege_type='UPDATE' AND grantee<>'PUBLIC'
    LOOP
      EXECUTE format('GRANT SELECT, INSERT ON chat_ack_requirement TO %I',role_name);
      EXECUTE format('GRANT SELECT, INSERT ON chat_ack_record TO %I',role_name);
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE chat_ack_record;
DROP TABLE chat_ack_requirement;
