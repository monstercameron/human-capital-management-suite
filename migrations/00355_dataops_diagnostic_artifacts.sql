-- REV-030-01: immutable tenant-scoped material for digest-bound DataOps follow-ups.
-- The payload contains the exact comparison inputs behind a diff or repair plan;
-- follow-up requests resolve by digest and never supply replacement authority.
--
-- +goose Up

CREATE TABLE IF NOT EXISTS dataops_diagnostic_artifact (
    tenant_id       tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    artifact_kind   text NOT NULL,
    artifact_digest text NOT NULL,
    payload         bytea NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, artifact_kind, artifact_digest),
    CONSTRAINT dataops_diagnostic_artifact_kind_valid CHECK (artifact_kind IN ('diff', 'repair_plan')),
    CONSTRAINT dataops_diagnostic_artifact_digest_valid CHECK (artifact_digest <> ''),
    CONSTRAINT dataops_diagnostic_artifact_payload_bound CHECK (octet_length(payload) <= 16777216)
);

ALTER TABLE dataops_diagnostic_artifact ENABLE ROW LEVEL SECURITY;
ALTER TABLE dataops_diagnostic_artifact FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON dataops_diagnostic_artifact
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE TRIGGER dataops_diagnostic_artifact_forbid_mutation
    BEFORE UPDATE OR DELETE ON dataops_diagnostic_artifact
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON dataops_diagnostic_artifact FROM PUBLIC;
REVOKE UPDATE, DELETE ON dataops_diagnostic_artifact FROM hcmnext_app;
GRANT SELECT, INSERT ON dataops_diagnostic_artifact TO hcmnext_app;

-- +goose Down

REVOKE ALL ON dataops_diagnostic_artifact FROM hcmnext_app;
DROP POLICY tenant_isolation ON dataops_diagnostic_artifact;
ALTER TABLE dataops_diagnostic_artifact NO FORCE ROW LEVEL SECURITY;
ALTER TABLE dataops_diagnostic_artifact DISABLE ROW LEVEL SECURITY;
DROP TRIGGER dataops_diagnostic_artifact_forbid_mutation ON dataops_diagnostic_artifact;
DROP TABLE dataops_diagnostic_artifact;
