-- +goose Up
-- Issue 3998: a reviewer's request for informal input, asked in a conversation
-- about one proposal revision of one workflow case. The row is the record that
-- the question was asked and what the reviewer saw before sending: where, who
-- could read it (kind and size), which revision it addressed, and a digest of
-- the words. The words stay in the conversation and follow its access and
-- retention. It is never an approval: no workflow reads this table to decide
-- anything. Rows are written once and kept; a retry with the same key finds it.
CREATE TABLE chat_input_request (
    tenant_id text NOT NULL,
    id text NOT NULL CHECK (btrim(id) <> ''),
    journey_id text NOT NULL CHECK (btrim(journey_id) <> ''),
    proposal_revision text NOT NULL CHECK (btrim(proposal_revision) <> ''),
    conversation_id text NOT NULL CHECK (btrim(conversation_id) <> ''),
    post_id text NOT NULL DEFAULT '',
    asked_by text NOT NULL CHECK (btrim(asked_by) <> ''),
    audience_kind text NOT NULL CHECK (audience_kind IN ('direct','group','private_channel','public_channel')),
    audience_size integer NOT NULL DEFAULT 0 CHECK (audience_size >= 0),
    shared_case_name boolean NOT NULL DEFAULT false,
    question_digest text NOT NULL CHECK (btrim(question_digest) <> ''),
    idempotency_key text NOT NULL CHECK (btrim(idempotency_key) <> ''),
    asked_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, asked_by, idempotency_key)
);
CREATE INDEX chat_input_request_journey ON chat_input_request(tenant_id, journey_id, asked_by, asked_at DESC);
ALTER TABLE chat_input_request ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_input_request FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_input_request
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));
CREATE TRIGGER chat_input_request_immutable BEFORE UPDATE OR DELETE ON chat_input_request FOR EACH ROW EXECUTE FUNCTION chat_forbid_mutation();

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
      EXECUTE format('GRANT SELECT, INSERT ON chat_input_request TO %I',role_name);
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE chat_input_request;
