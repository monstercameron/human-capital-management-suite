-- AGENT-026/AGENTP-012 immutable, tenant-scoped validated persona output.
-- The payload is accepted only through the server-owned FinalOutputHandle API.
-- +goose Up

CREATE TABLE persona_final_outputs (
    tenant_id             uuid        NOT NULL REFERENCES tenant(tenant_id),
    invocation_id         text        NOT NULL CHECK (btrim(invocation_id) <> ''),
    output_id             text        NOT NULL CHECK (btrim(output_id) <> ''),
    invoker_id            text        NOT NULL CHECK (btrim(invoker_id) <> ''),
    conversation_id       text        NOT NULL CHECK (btrim(conversation_id) <> ''),
    thread_id             text        NOT NULL CHECK (btrim(thread_id) <> ''),
    parent_post_id        text        NOT NULL CHECK (btrim(parent_post_id) <> ''),
    persona_id            text        NOT NULL CHECK (btrim(persona_id) <> ''),
    persona_version       text        NOT NULL CHECK (btrim(persona_version) <> ''),
    installation_id       text        NOT NULL CHECK (btrim(installation_id) <> ''),
    admission_digest      text        NOT NULL CHECK (admission_digest ~ '^sha256:[0-9a-f]{64}$'),
    agent026_digest       text        NOT NULL CHECK (agent026_digest ~ '^sha256:[0-9a-f]{64}$'),
    persistence_digest    text        NOT NULL CHECK (persistence_digest ~ '^sha256:[0-9a-f]{64}$'),
    recovery_receipt      bytea       NOT NULL CHECK (octet_length(recovery_receipt) > 0),
    materials             jsonb       NOT NULL CHECK (jsonb_typeof(materials) = 'array'),
    citations             jsonb       NOT NULL CHECK (jsonb_typeof(citations) = 'array'),
    sealed_payload        jsonb       NOT NULL CHECK (jsonb_typeof(sealed_payload) = 'object'),
    created_at            timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, output_id),
    UNIQUE (tenant_id, invocation_id, output_id)
);

CREATE INDEX persona_final_outputs_invocation
    ON persona_final_outputs (tenant_id, invocation_id, created_at, output_id);

-- A final output is a sealed evidence record. Corrections create a new output
-- identity; mutating an existing row would invalidate the AGENT-026 digest.
CREATE TRIGGER persona_final_output_guard
    BEFORE UPDATE OR DELETE ON persona_final_outputs
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE persona_final_outputs ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_final_outputs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_final_outputs
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON persona_final_outputs TO hcmnext_agent_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM persona_final_outputs) THEN
        RAISE EXCEPTION 'cannot remove retained persona final output evidence';
    END IF;
END $$;
-- +goose StatementEnd
DROP TRIGGER persona_final_output_guard ON persona_final_outputs;
DROP TABLE persona_final_outputs;
