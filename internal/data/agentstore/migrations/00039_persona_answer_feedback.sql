-- +goose Up
CREATE TABLE persona_answer_feedback (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    output_id text NOT NULL CHECK (btrim(output_id) <> ''),
    person_id text NOT NULL CHECK (btrim(person_id) <> ''),
    helpful boolean,
    reason text NOT NULL DEFAULT '' CHECK (char_length(reason) <= 500),
    active boolean NOT NULL,
    revision bigint NOT NULL CHECK (revision > 0),
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, output_id, person_id),
    FOREIGN KEY (tenant_id, output_id) REFERENCES persona_final_outputs(tenant_id, output_id),
    CHECK ((active AND helpful IS NOT NULL) OR (NOT active AND helpful IS NULL AND reason = ''))
);
ALTER TABLE persona_answer_feedback ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_answer_feedback FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_answer_feedback
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON persona_answer_feedback TO hcmnext_agent_app;

CREATE TABLE persona_answer_feedback_request (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    output_id text NOT NULL CHECK (btrim(output_id) <> ''),
    person_id text NOT NULL CHECK (btrim(person_id) <> ''),
    idempotency_key text NOT NULL CHECK (char_length(idempotency_key) BETWEEN 8 AND 128 AND btrim(idempotency_key) = idempotency_key),
    helpful boolean,
    reason text NOT NULL DEFAULT '' CHECK (char_length(reason) <= 500),
    active boolean NOT NULL,
    occurred_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, output_id, person_id, idempotency_key),
    FOREIGN KEY (tenant_id, output_id) REFERENCES persona_final_outputs(tenant_id, output_id),
    CHECK ((active AND helpful IS NOT NULL) OR (NOT active AND helpful IS NULL AND reason = ''))
);
CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON persona_answer_feedback_request
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
ALTER TABLE persona_answer_feedback_request ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_answer_feedback_request FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_answer_feedback_request
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON persona_answer_feedback_request TO hcmnext_agent_app;

-- +goose Down
DROP TABLE persona_answer_feedback_request;
DROP TABLE persona_answer_feedback;
