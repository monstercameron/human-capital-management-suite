-- AGENTUX-075: an agent reacts to the question it is asked. Its owner can turn
-- that off for the agent; one row per persona holds the choice. No row means the
-- agent reacts (the default), so existing agents are unchanged.
-- +goose Up
CREATE TABLE persona_reaction_setting (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    persona_id text NOT NULL CHECK (btrim(persona_id) <> '' AND char_length(persona_id) <= 512),
    react_to_questions boolean NOT NULL,
    revision bigint NOT NULL CHECK (revision > 0),
    updated_by text NOT NULL CHECK (btrim(updated_by) <> ''),
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, persona_id)
);
ALTER TABLE persona_reaction_setting ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_reaction_setting FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_reaction_setting
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON persona_reaction_setting TO hcmnext_agent_app;

-- +goose Down
DROP TABLE persona_reaction_setting;
