-- WF-DATA-037: retain immutable environment-specific tenant parameter values.

-- +goose Up

CREATE TABLE tenant_parameter_value_revision (
    tenant_id          tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    parameter_key      text NOT NULL CHECK (parameter_key ~ '^[a-z][a-z0-9_-]*(\.[a-z][a-z0-9_-]*)+$'),
    scope_kind         text NOT NULL CHECK (scope_kind IN ('TENANT','COMPANY','LEGAL_ENTITY','ORGANIZATION')),
    scope_id           text NOT NULL CHECK (length(btrim(scope_id)) > 0),
    environment        text NOT NULL CHECK (environment IN ('SANDBOX','PRODUCTION')),
    revision           bigint NOT NULL CHECK (revision > 0),
    value_text         text NOT NULL,
    value_type         jsonb NOT NULL CHECK (jsonb_typeof(value_type) = 'object'),
    definition_name    text NOT NULL,
    definition_version text NOT NULL,
    author             text NOT NULL CHECK (length(btrim(author)) > 0),
    reason             text NOT NULL CHECK (length(btrim(reason)) > 0),
    recorded_at        timestamptz NOT NULL,
    locked             boolean NOT NULL DEFAULT false,
    PRIMARY KEY (tenant_id, parameter_key, scope_kind, scope_id, environment, revision)
);

ALTER TABLE tenant_parameter_value_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_parameter_value_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON tenant_parameter_value_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE TRIGGER tenant_parameter_value_revision_forbid_mutation
    BEFORE UPDATE OR DELETE ON tenant_parameter_value_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON tenant_parameter_value_revision FROM PUBLIC;
REVOKE UPDATE, DELETE ON tenant_parameter_value_revision FROM hcmnext_app;
GRANT SELECT, INSERT ON tenant_parameter_value_revision TO hcmnext_app;

-- +goose Down

REVOKE ALL ON tenant_parameter_value_revision FROM hcmnext_app;
DROP POLICY tenant_isolation ON tenant_parameter_value_revision;
ALTER TABLE tenant_parameter_value_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE tenant_parameter_value_revision DISABLE ROW LEVEL SECURITY;
DROP TRIGGER tenant_parameter_value_revision_forbid_mutation ON tenant_parameter_value_revision;
DROP TABLE tenant_parameter_value_revision;
