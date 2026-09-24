-- REV-030-01: durable, fully versioned INTG-001 connector publications.
-- scope_id is empty for tenant-wide publications and the principal's stable
-- organization-scope reference for an organization-scoped publication.
-- +goose Up

CREATE TABLE published_connector_definition (
    tenant_id uuid NOT NULL REFERENCES tenant (tenant_id),
    scope_id text NOT NULL,
    connector_id semantic_key NOT NULL,
    version_major bigint NOT NULL,
    version_minor bigint NOT NULL,
    version_patch bigint NOT NULL,
    definition jsonb NOT NULL,
    definition_digest text NOT NULL,
    published_by text NOT NULL,
    published_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, scope_id, connector_id, version_major, version_minor, version_patch),
    CONSTRAINT published_connector_scope_valid CHECK (scope_id = btrim(scope_id)),
    CONSTRAINT published_connector_version_positive CHECK (
        version_major BETWEEN 0 AND 4294967295
        AND version_minor BETWEEN 0 AND 4294967295
        AND version_patch BETWEEN 0 AND 4294967295
        AND (version_major > 0 OR version_minor > 0 OR version_patch > 0)
    ),
    CONSTRAINT published_connector_definition_object CHECK (jsonb_typeof(definition) = 'object'),
    CONSTRAINT published_connector_digest_shape CHECK (definition_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT published_connector_publisher_present CHECK (length(btrim(published_by)) > 0)
);

CREATE INDEX published_connector_definition_scope
    ON published_connector_definition (tenant_id, scope_id, connector_id, version_major, version_minor, version_patch);
ALTER TABLE published_connector_definition ENABLE ROW LEVEL SECURITY;
ALTER TABLE published_connector_definition FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON published_connector_definition
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE TRIGGER published_connector_definition_forbid_mutation
    BEFORE UPDATE OR DELETE ON published_connector_definition
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON published_connector_definition FROM PUBLIC;
REVOKE UPDATE, DELETE ON published_connector_definition FROM hcmnext_app;
GRANT SELECT, INSERT ON published_connector_definition TO hcmnext_app;

-- +goose Down
REVOKE ALL ON published_connector_definition FROM hcmnext_app;
DROP POLICY tenant_isolation ON published_connector_definition;
ALTER TABLE published_connector_definition NO FORCE ROW LEVEL SECURITY;
ALTER TABLE published_connector_definition DISABLE ROW LEVEL SECURITY;
DROP TRIGGER published_connector_definition_forbid_mutation ON published_connector_definition;
DROP TABLE published_connector_definition;
