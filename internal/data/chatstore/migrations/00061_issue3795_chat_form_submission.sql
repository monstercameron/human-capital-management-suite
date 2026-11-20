-- +goose Up
-- Issue 3795: the answer to a form card. A form card is a message whose body
-- holds the form's fields; the answers a person gives are kept here, one row
-- per card, written once. The answers are private to the person who gave them
-- and the agent that asked: no other reader of the conversation is shown them,
-- only the fact that the form was answered, which the message list reads.
CREATE TABLE chat_form_submission (
    tenant_id text NOT NULL,
    conversation_id text NOT NULL,
    post_id text NOT NULL,
    submitted_by_id text NOT NULL CHECK (btrim(submitted_by_id) <> ''),
    requested_by_id text NOT NULL CHECK (btrim(requested_by_id) <> ''),
    answers jsonb NOT NULL CHECK (jsonb_typeof(answers) = 'object'),
    submitted_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, conversation_id, post_id),
    FOREIGN KEY (tenant_id, conversation_id) REFERENCES chat_conversation(tenant_id, id) ON DELETE CASCADE
);
CREATE INDEX chat_form_submission_submitter ON chat_form_submission(tenant_id, submitted_by_id, submitted_at DESC);
ALTER TABLE chat_form_submission ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_form_submission FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_form_submission
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));
CREATE TRIGGER chat_form_submission_immutable BEFORE UPDATE ON chat_form_submission FOR EACH ROW EXECUTE FUNCTION chat_forbid_mutation();

-- +goose Down
DROP TABLE chat_form_submission;
