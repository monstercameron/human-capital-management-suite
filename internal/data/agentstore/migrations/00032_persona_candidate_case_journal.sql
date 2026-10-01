-- AGENTP-021: evaluator-owned immutable synthetic runtime checkpoints.
-- +goose Up
-- +goose StatementBegin
DO $$
BEGIN
    CREATE ROLE hcmnext_agent_eval_runtime
        NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOLOGIN NOREPLICATION NOBYPASSRLS;
EXCEPTION WHEN duplicate_object OR unique_violation THEN NULL;
END
$$;
-- +goose StatementEnd
ALTER ROLE hcmnext_agent_eval_runtime
    NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOLOGIN NOREPLICATION NOBYPASSRLS;
-- +goose StatementBegin
DO $$
DECLARE target_schema text := current_schema();
BEGIN
    EXECUTE format('GRANT USAGE ON SCHEMA %I TO hcmnext_agent_eval_runtime', target_schema);
END
$$;
-- +goose StatementEnd
CREATE TABLE persona_candidate_case_journal (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    invocation_id text NOT NULL CHECK (btrim(invocation_id) <> ''),
    production_tenant_id uuid NOT NULL,
    persona_id text NOT NULL CHECK (btrim(persona_id) <> ''),
    persona_version bigint NOT NULL CHECK (persona_version > 0),
    profile_digest text NOT NULL CHECK (profile_digest ~ '^sha256:[0-9a-f]{64}$'),
    model_digest text NOT NULL CHECK (model_digest ~ '^sha256:[0-9a-f]{64}$'),
    case_digest text NOT NULL CHECK (case_digest ~ '^sha256:[0-9a-f]{64}$'),
    evidence_digest text NOT NULL CHECK (evidence_digest ~ '^sha256:[0-9a-f]{64}$'),
    record jsonb NOT NULL CHECK (jsonb_typeof(record) = 'object'),
    completed_at timestamptz NOT NULL,
    PRIMARY KEY(tenant_id,invocation_id),
    CHECK (tenant_id <> production_tenant_id)
);
CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON persona_candidate_case_journal
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
ALTER TABLE persona_candidate_case_journal ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_candidate_case_journal FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_candidate_case_journal
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT ON persona_candidate_case_journal TO hcmnext_agent_app;
GRANT SELECT, INSERT ON persona_candidate_case_journal TO hcmnext_agent_eval_runtime;
-- +goose Down
DROP TABLE persona_candidate_case_journal;
-- +goose StatementBegin
DO $$
DECLARE target_schema text := current_schema();
BEGIN
    EXECUTE format('REVOKE USAGE ON SCHEMA %I FROM hcmnext_agent_eval_runtime', target_schema);
END
$$;
-- +goose StatementEnd
DROP ROLE hcmnext_agent_eval_runtime;
