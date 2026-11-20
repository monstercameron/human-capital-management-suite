-- +goose Up
-- Issue 3995: an approver's request for clarification, asked in a private
-- conversation about one proposal revision of one workflow case. The row is the
-- governed record that the question was asked: who asked, of whom, about which
-- revision, where, and a digest of the question as sent. The question's words
-- stay in the conversation (and follow its access and retention), so editing
-- or deleting that message never rewrites what was asked. A reply in chat is
-- not recorded here: only the owning workflow can accept new information.
-- Rows are written once and kept; a retry with the same key finds the row.
CREATE TABLE chat_approval_clarification (
    tenant_id text NOT NULL,
    id text NOT NULL CHECK (btrim(id) <> ''),
    journey_id text NOT NULL CHECK (btrim(journey_id) <> ''),
    proposal_revision text NOT NULL CHECK (btrim(proposal_revision) <> ''),
    conversation_id text NOT NULL CHECK (btrim(conversation_id) <> ''),
    post_id text NOT NULL DEFAULT '',
    asked_by text NOT NULL CHECK (btrim(asked_by) <> ''),
    recipient_id text NOT NULL CHECK (btrim(recipient_id) <> '' AND recipient_id <> asked_by),
    question_digest text NOT NULL CHECK (btrim(question_digest) <> ''),
    idempotency_key text NOT NULL CHECK (btrim(idempotency_key) <> ''),
    asked_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, asked_by, idempotency_key)
);
CREATE INDEX chat_approval_clarification_journey ON chat_approval_clarification(tenant_id, journey_id, asked_at DESC);
ALTER TABLE chat_approval_clarification ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_approval_clarification FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_approval_clarification
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));
CREATE TRIGGER chat_approval_clarification_immutable BEFORE UPDATE OR DELETE ON chat_approval_clarification FOR EACH ROW EXECUTE FUNCTION chat_forbid_mutation();

-- The store writes a request once and reads it. It never changes or deletes one.
-- A serving role is given what the store runs against the table and nothing
-- more, whether its privileges came from the schema's defaults or from a grant
-- made before this table existed (migration 00041 has the history). Row level
-- security still scopes every statement to the caller's tenant.
-- +goose StatementBegin
DO $$ DECLARE role_name text; BEGIN
    FOR role_name IN SELECT DISTINCT grantee FROM information_schema.role_table_grants
      WHERE table_schema=current_schema() AND table_name='chat_post' AND privilege_type='UPDATE' AND grantee<>'PUBLIC'
    LOOP
      EXECUTE format('GRANT SELECT, INSERT ON chat_approval_clarification TO %I',role_name);
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE chat_approval_clarification;
