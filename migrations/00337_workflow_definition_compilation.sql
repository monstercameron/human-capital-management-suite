-- WF-EXT-008: bind a tenant's immutable workflow definition revision to the
-- exact compiled version produced at publication. Activation remains the
-- existing tenant-scoped definition_active_pointer.

-- +goose Up

CREATE TABLE workflow_definition_compilation (
    tenant_id          tenant_ref NOT NULL,
    definition_kind    text NOT NULL DEFAULT 'WORKFLOW',
    definition_key     semantic_key NOT NULL,
    definition_version cas_version NOT NULL,
    compiled_plan_digest text NOT NULL REFERENCES workflow_compiled_version (compiled_plan_digest),
    recorded_at        timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, definition_key, definition_version),
    FOREIGN KEY (tenant_id, definition_kind, definition_key, definition_version)
        REFERENCES definition_version (tenant_id, definition_kind, definition_key, version),
    CONSTRAINT workflow_definition_compilation_kind CHECK (definition_kind = 'WORKFLOW')
);

CREATE TRIGGER workflow_definition_compilation_append_only
    BEFORE UPDATE OR DELETE ON workflow_definition_compilation
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE workflow_definition_compilation ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_definition_compilation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workflow_definition_compilation
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON workflow_definition_compilation TO hcmnext_app;
GRANT SELECT, INSERT ON definition_version TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON definition_active_pointer TO hcmnext_app;

-- +goose Down
DROP TABLE workflow_definition_compilation;
