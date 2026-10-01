-- +goose Up
CREATE TABLE persona_invocation_failure (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    invoker_id text NOT NULL CHECK (btrim(invoker_id) <> ''),
    post_id text NOT NULL CHECK (btrim(post_id) <> ''),
    conversation_id text NOT NULL CHECK (btrim(conversation_id) <> ''),
    thread_id text NOT NULL CHECK (btrim(thread_id) <> ''),
    failure_code text NOT NULL CHECK (failure_code IN ('MODEL_UNAVAILABLE','OUTPUT_REJECTED','DELIVERY_FAILED','ADMISSION_REFUSED','ADMISSION_UNAVAILABLE','EXECUTION_UNAVAILABLE','INVOCATION_FAILED')),
    retryable boolean NOT NULL,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, invoker_id, post_id),
    CHECK (NOT retryable OR failure_code IN ('MODEL_UNAVAILABLE','ADMISSION_UNAVAILABLE','EXECUTION_UNAVAILABLE'))
);
CREATE INDEX persona_invocation_failure_owner ON persona_invocation_failure (tenant_id, invoker_id, conversation_id, occurred_at DESC, post_id);
ALTER TABLE persona_invocation_failure ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_invocation_failure FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_invocation_failure USING (tenant_id = NULLIF(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id',true),'')::uuid);
CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON persona_invocation_failure FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
GRANT SELECT, INSERT ON persona_invocation_failure TO hcmnext_agent_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM persona_invocation_failure) THEN
        RAISE EXCEPTION 'cannot remove retained persona invocation failures';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE persona_invocation_failure;
