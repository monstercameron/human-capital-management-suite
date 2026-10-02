-- +goose Up
CREATE TABLE support_project_grant (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    service_subject text NOT NULL CHECK (btrim(service_subject) <> ''),
    project_id text NOT NULL CHECK (btrim(project_id) <> ''),
    revision bigint NOT NULL CHECK (revision > 0),
    active boolean NOT NULL,
    granted_by text NOT NULL CHECK (btrim(granted_by) <> ''),
    recorded_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id,service_subject,project_id,revision)
);
CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON support_project_grant
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
ALTER TABLE support_project_grant ENABLE ROW LEVEL SECURITY;
ALTER TABLE support_project_grant FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON support_project_grant
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT ON support_project_grant TO hcmnext_agent_app;

-- +goose Down
DROP TABLE support_project_grant;
