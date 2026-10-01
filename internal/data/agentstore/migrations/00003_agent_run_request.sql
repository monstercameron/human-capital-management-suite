-- AGENT-015 durable admission inbox. Cross-database handoffs commit here
-- before inference; source-key uniqueness makes redelivery idempotent.
-- +goose Up

CREATE TABLE agent_run_request (
    tenant_id           uuid        NOT NULL REFERENCES tenant (tenant_id),
    request_id          text        NOT NULL CHECK (btrim(request_id) <> ''),
    source_kind         text        NOT NULL CHECK (source_kind IN ('CHAT','PERSONA_MENTION','API','EVENT','SCHEDULE','WORKFLOW')),
    source_key_digest   char(64)    NOT NULL CHECK (source_key_digest ~ '^[0-9a-f]{64}$'),
    source_ref          text        CHECK (source_ref IS NULL OR btrim(source_ref) <> ''),
    request_digest      char(64)    NOT NULL CHECK (request_digest ~ '^[0-9a-f]{64}$'),
    request_payload     jsonb       NOT NULL CHECK (jsonb_typeof(request_payload) = 'object'),
    decision            text        NOT NULL CHECK (decision IN ('ACCEPTED','REFUSED')),
    refusal_code        text        CHECK (refusal_code IS NULL OR btrim(refusal_code) <> ''),
    authority_snapshot  jsonb       CHECK (authority_snapshot IS NULL OR jsonb_typeof(authority_snapshot) = 'object'),
    admitted_at         timestamptz NOT NULL,
    deadline            timestamptz NOT NULL,
    agent_id            text        NOT NULL CHECK (btrim(agent_id) <> ''),
    agent_version       text        NOT NULL CHECK (btrim(agent_version) <> ''),
    agent_digest        char(71)    NOT NULL CHECK (agent_digest ~ '^sha256:[0-9a-f]{64}$'),
    installation_id     text        NOT NULL CHECK (btrim(installation_id) <> ''),
    legal_entity_id     text        NOT NULL DEFAULT '',
    principal_chain     jsonb       NOT NULL CHECK (
        jsonb_typeof(principal_chain) = 'object'
        AND COALESCE(principal_chain->>'mode' IN ('ON_BEHALF_OF','SPONSORED'), false)
    ),
    purpose             text        NOT NULL CHECK (btrim(purpose) <> ''),
    audience            jsonb       NOT NULL CHECK (jsonb_typeof(audience) IN ('array','object')),
    context_scope       jsonb       NOT NULL CHECK (jsonb_typeof(context_scope) IN ('array','object')),
    budget              jsonb       NOT NULL CHECK (jsonb_typeof(budget) = 'object'),
    cause_id            text        NOT NULL CHECK (btrim(cause_id) <> ''),
    PRIMARY KEY (tenant_id, request_id),
    UNIQUE (tenant_id, source_kind, source_key_digest),
    CHECK (
        (decision = 'ACCEPTED' AND refusal_code IS NULL AND authority_snapshot IS NOT NULL)
        OR
        (decision = 'REFUSED' AND refusal_code IS NOT NULL AND authority_snapshot IS NULL)
    )
);

CREATE INDEX agent_run_request_admitted ON agent_run_request (tenant_id, admitted_at, request_id);

ALTER TABLE agent_run_request ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_run_request FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_run_request
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE TRIGGER agent_run_request_immutable
    BEFORE UPDATE OR DELETE ON agent_run_request
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

GRANT SELECT, INSERT ON agent_run_request TO hcmnext_agent_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM agent_run_request) THEN
        RAISE EXCEPTION 'cannot remove retained agent run requests';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE agent_run_request;
