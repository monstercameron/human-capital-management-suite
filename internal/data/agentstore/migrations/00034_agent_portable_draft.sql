-- Portable imports are durable reviewable drafts. They carry no installation,
-- grant, schedule, or execution state.
-- +goose Up

CREATE TABLE agent_portable_draft (
    tenant_id uuid NOT NULL,
    definition_id text NOT NULL,
    version bigint NOT NULL,
    state text NOT NULL CHECK (state = 'DRAFT'),
    actor_id text NOT NULL CHECK (btrim(actor_id) <> ''),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, definition_id, version),
    FOREIGN KEY (tenant_id, definition_id, version)
        REFERENCES agent_definition_version (tenant_id, definition_id, version)
);

ALTER TABLE agent_portable_draft ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_portable_draft FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_portable_draft
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE TRIGGER agent_portable_draft_immutable
    BEFORE UPDATE OR DELETE ON agent_portable_draft
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
GRANT SELECT, INSERT ON agent_portable_draft TO hcmnext_agent_app;

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM agent_portable_draft) THEN
        RAISE EXCEPTION 'cannot remove agent portable drafts while retained records exist';
    END IF;
END
$$;
-- +goose StatementEnd
DROP TABLE agent_portable_draft;
