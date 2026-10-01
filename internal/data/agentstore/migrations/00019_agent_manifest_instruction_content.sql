-- AGENT-007: tenant-owned immutable executable instructions pinned by digest.
-- +goose Up

CREATE TABLE agent_instruction_content (
    tenant_id uuid NOT NULL REFERENCES tenant (tenant_id),
    digest text NOT NULL CHECK (digest ~ '^sha256:[0-9a-f]{64}$'),
    content text NOT NULL CHECK (btrim(content) <> ''),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, digest)
);

CREATE TRIGGER agent_instruction_content_immutable
    BEFORE UPDATE OR DELETE ON agent_instruction_content
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE agent_instruction_content ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_instruction_content FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_instruction_content
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON agent_instruction_content TO hcmnext_agent_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM agent_instruction_content) THEN
        RAISE EXCEPTION 'cannot remove retained agent instruction content';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE agent_instruction_content;
