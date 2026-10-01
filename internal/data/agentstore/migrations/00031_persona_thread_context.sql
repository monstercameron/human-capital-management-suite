-- AGENTP-008/011: exact admitted bounded source context for durable recovery.
-- Reader labels in the payload are provenance, never workload or human credentials.
-- +goose Up
CREATE TABLE persona_thread_context (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    snapshot_id text NOT NULL CHECK (btrim(snapshot_id) <> ''),
    digest text NOT NULL CHECK (digest ~ '^sha256:[0-9a-f]{64}$'),
    context_payload bytea NOT NULL CHECK (octet_length(context_payload) > 0 AND octet_length(context_payload) <= 1048576),
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (tenant_id, snapshot_id)
);
CREATE TRIGGER persona_thread_context_guard BEFORE UPDATE OR DELETE ON persona_thread_context
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
ALTER TABLE persona_thread_context ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_thread_context FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_thread_context
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON persona_thread_context TO hcmnext_agent_app;
-- +goose Down
DROP TABLE persona_thread_context;
