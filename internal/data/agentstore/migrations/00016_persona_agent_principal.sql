-- AGENT-015 binds each immutable persona version to an explicitly provisioned
-- service principal. The core trust database owns and validates principal_id.
-- The agent database stores only the tenant-scoped exact-version reference.
-- +goose Up

CREATE TABLE persona_agent_principal_binding (
    tenant_id       uuid        NOT NULL REFERENCES tenant(tenant_id),
    persona_id      text        NOT NULL CHECK (btrim(persona_id) <> ''),
    persona_version bigint      NOT NULL CHECK (persona_version > 0),
    principal_id    uuid        NOT NULL,
    provisioned_at  timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, persona_id, persona_version),
    FOREIGN KEY (tenant_id, persona_id, persona_version)
        REFERENCES persona_versions (tenant_id, persona_id, version)
);

ALTER TABLE persona_agent_principal_binding ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_agent_principal_binding FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_agent_principal_binding
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- Bindings are immutable. Provisioning runs through the explicitly authorized
-- agent control plane; regular run admission is read-only.
CREATE TRIGGER persona_agent_principal_binding_immutable
    BEFORE UPDATE OR DELETE ON persona_agent_principal_binding
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
GRANT SELECT, INSERT ON persona_agent_principal_binding TO hcmnext_agent_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM persona_agent_principal_binding) THEN
        RAISE EXCEPTION 'cannot remove retained persona agent principal binding';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE persona_agent_principal_binding;
