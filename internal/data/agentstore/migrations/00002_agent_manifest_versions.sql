-- AGENT-007 immutable manifest versions and revision-fenced current pointer.
-- +goose Up

CREATE TABLE agent_definition_version (
    tenant_id      uuid        NOT NULL REFERENCES tenant (tenant_id),
    definition_id  text        NOT NULL CHECK (btrim(definition_id) <> ''),
    version        bigint      NOT NULL CHECK (version > 0),
    schema_version integer     NOT NULL CHECK (schema_version > 0),
    digest         text        NOT NULL CHECK (digest ~ '^sha256:[0-9a-f]{64}$'),
    manifest       jsonb       NOT NULL CHECK (jsonb_typeof(manifest) = 'object'),
    created_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, definition_id, version)
);

CREATE TABLE agent_definition_head (
    tenant_id       uuid        NOT NULL REFERENCES tenant (tenant_id),
    definition_id   text        NOT NULL CHECK (btrim(definition_id) <> ''),
    current_version bigint     NOT NULL CHECK (current_version > 0),
    revision        bigint      NOT NULL CHECK (revision > 0),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, definition_id),
    FOREIGN KEY (tenant_id, definition_id, current_version)
        REFERENCES agent_definition_version (tenant_id, definition_id, version)
);

CREATE TRIGGER agent_definition_version_immutable
    BEFORE UPDATE OR DELETE ON agent_definition_version
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE agent_definition_version ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_definition_version FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_definition_version
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE agent_definition_head ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_definition_head FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_definition_head
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON agent_definition_version TO hcmnext_agent_app;
GRANT SELECT, INSERT, UPDATE ON agent_definition_head TO hcmnext_agent_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM agent_definition_version) THEN
        RAISE EXCEPTION 'cannot remove retained agent definition versions';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE agent_definition_head;
DROP TABLE agent_definition_version;
