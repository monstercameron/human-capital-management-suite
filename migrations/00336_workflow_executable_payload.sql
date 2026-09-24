-- 00336: immutable rule-table, expression and transformation IR payloads.
--
-- Registry metadata and executable bodies have separate responsibilities.
-- This table retains the complete typed payload under its exact tenant,
-- reference kind, ID and version; compiled workflow references also pin the
-- SHA-256 digest so resolution cannot silently substitute a later body.

-- +goose Up

CREATE TABLE workflow_executable_payload (
    tenant_id       tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    reference_kind  text       NOT NULL CHECK (reference_kind IN ('RULE', 'TRANSFORM')),
    reference_id    text       NOT NULL CHECK (reference_id <> ''),
    reference_version text     NOT NULL CHECK (reference_version <> ''),
    payload_kind    text       NOT NULL CHECK (payload_kind IN ('DECISION_TABLE', 'EXPRESSION', 'TRANSFORM_IR')),
    digest          text       NOT NULL CHECK (digest LIKE 'sha256:%'),
    payload         jsonb      NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    published_at    timestamptz NOT NULL DEFAULT clock_timestamp(),

    PRIMARY KEY (tenant_id, reference_kind, reference_id, reference_version)
);

ALTER TABLE workflow_executable_payload ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_executable_payload FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workflow_executable_payload
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

CREATE TRIGGER workflow_executable_payload_append_only
    BEFORE UPDATE OR DELETE ON workflow_executable_payload
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

GRANT SELECT, INSERT ON workflow_executable_payload TO hcmnext_app;

-- +goose Down

-- Published program bodies are replay and audit evidence; removing one would
-- make an exact compiled reference impossible to resolve.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00336 is irreversible: published executable payloads are immutable references'; END $$;
-- +goose StatementEnd
