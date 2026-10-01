-- Semantic policies may deploy independently for several immutable agent versions.
-- +goose Up
CREATE TABLE persona_model_route_deployment (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    legal_entity_id text NOT NULL CHECK(btrim(legal_entity_id) <> ''),
    policy_id text NOT NULL CHECK(btrim(policy_id) <> ''),
    policy_version bigint NOT NULL CHECK(policy_version > 0),
    policy_schema_version integer NOT NULL CHECK(policy_schema_version > 0),
    policy_digest text NOT NULL CHECK(policy_digest ~ '^sha256:[0-9a-f]{64}$'),
    revision bigint NOT NULL CHECK(revision > 0),
    effective_from timestamptz NOT NULL,
    effective_until timestamptz NOT NULL,
    route_payload jsonb NOT NULL CHECK(jsonb_typeof(route_payload) = 'object'),
    route_payload_digest text NOT NULL CHECK(route_payload_digest ~ '^sha256:[0-9a-f]{64}$'),
    policy_payload jsonb NOT NULL CHECK(jsonb_typeof(policy_payload) = 'object'),
    evaluation_run_id text NOT NULL CHECK(btrim(evaluation_run_id) <> ''),
    evaluation_model_digest text NOT NULL CHECK(evaluation_model_digest ~ '^sha256:[0-9a-f]{64}$'),
    agent_version_digest text NOT NULL CHECK (agent_version_digest ~ '^sha256:[0-9a-f]{64}$'),
    PRIMARY KEY(tenant_id,legal_entity_id,policy_id,policy_version,policy_schema_version,agent_version_digest,revision),
    CHECK(effective_until > effective_from)
);
CREATE INDEX persona_model_route_deployment_effective ON persona_model_route_deployment(tenant_id,legal_entity_id,policy_id,policy_version,policy_schema_version,agent_version_digest,effective_from DESC);
CREATE TRIGGER persona_model_route_deployment_immutable BEFORE UPDATE OR DELETE ON persona_model_route_deployment FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
ALTER TABLE persona_model_route_deployment ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_model_route_deployment FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_model_route_deployment
    USING(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
    WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);
GRANT SELECT ON persona_model_route_deployment TO hcmnext_agent_app;
GRANT SELECT,INSERT ON persona_model_route_deployment TO hcmnext_persona_model_route_authority;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM persona_model_route_deployment) THEN
        RAISE EXCEPTION 'cannot remove retained exact agent model deployments';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE persona_model_route_deployment;
