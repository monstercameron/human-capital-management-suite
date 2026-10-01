-- Tenant and legal-entity owned model route selection for persona runs.
-- The payload pins one qualified model profile and its processing terms.
-- +goose Up

CREATE TABLE persona_model_route_policy (
    tenant_id         uuid        NOT NULL REFERENCES tenant (tenant_id),
    legal_entity_id   text        NOT NULL CHECK (btrim(legal_entity_id) <> ''),
    policy_id         text        NOT NULL CHECK (btrim(policy_id) <> ''),
    policy_version    bigint      NOT NULL CHECK (policy_version > 0),
    policy_schema_version integer NOT NULL CHECK (policy_schema_version > 0),
    policy_digest     text        NOT NULL CHECK (policy_digest ~ '^sha256:[0-9a-f]{64}$'),
    revision          bigint      NOT NULL CHECK (revision > 0),
    effective_from    timestamptz NOT NULL,
    effective_until   timestamptz,
    route_payload     jsonb       NOT NULL CHECK (jsonb_typeof(route_payload) = 'object'),
    PRIMARY KEY (tenant_id, legal_entity_id, policy_id, policy_version, policy_schema_version, revision),
    CHECK (effective_until IS NULL OR effective_until > effective_from)
);
CREATE INDEX persona_model_route_policy_effective
    ON persona_model_route_policy (tenant_id, legal_entity_id, policy_id, policy_version, policy_schema_version, effective_from DESC);

CREATE TRIGGER persona_model_route_policy_immutable
    BEFORE UPDATE OR DELETE ON persona_model_route_policy
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE persona_model_route_policy ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_model_route_policy FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_model_route_policy
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT ON persona_model_route_policy TO hcmnext_agent_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM persona_model_route_policy) THEN
        RAISE EXCEPTION 'cannot remove retained persona model route policy';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE persona_model_route_policy;
