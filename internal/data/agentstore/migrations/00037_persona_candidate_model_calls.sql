-- AGENTP-021: exact normalized model observations from real candidate dispatch.
-- +goose Up
CREATE TABLE persona_candidate_model_calls (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    task_id text NOT NULL CHECK (btrim(task_id) <> ''),
    step_id text NOT NULL CHECK (btrim(step_id) <> ''),
    evidence_digest text NOT NULL CHECK (evidence_digest ~ '^sha256:[0-9a-f]{64}$'),
    record jsonb NOT NULL CHECK (jsonb_typeof(record) = 'object'),
    completed_at timestamptz NOT NULL,
    PRIMARY KEY(tenant_id,task_id,step_id)
);
CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON persona_candidate_model_calls
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
ALTER TABLE persona_candidate_model_calls ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_candidate_model_calls FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_candidate_model_calls
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT ON persona_candidate_model_calls TO hcmnext_agent_app;
GRANT SELECT, INSERT ON persona_candidate_model_calls TO hcmnext_agent_eval_runtime;
-- +goose Down
DROP TABLE persona_candidate_model_calls;
