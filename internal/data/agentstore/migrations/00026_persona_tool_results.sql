-- AGENTP-008/011: local capability output bound to the accepted persona run.
-- +goose Up
CREATE TABLE persona_tool_results (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    run_id text NOT NULL CHECK (btrim(run_id) <> ''),
    tool_call_id text NOT NULL CHECK (btrim(tool_call_id) <> ''),
    record bytea NOT NULL CHECK (octet_length(record) > 0 AND octet_length(record) <= 131072),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, run_id, tool_call_id)
);
CREATE TRIGGER persona_tool_result_guard BEFORE UPDATE OR DELETE ON persona_tool_results
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
ALTER TABLE persona_tool_results ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_tool_results FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_tool_results
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON persona_tool_results TO hcmnext_agent_app;
-- +goose Down
DROP TABLE persona_tool_results;
