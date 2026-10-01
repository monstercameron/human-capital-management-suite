-- Current effective per-run cost, token, and wall-clock ceilings by tenant
-- legal entity. The trusted policy authority owns inserts; serving agents
-- receive read-only access and fail closed when no unique active row exists.
-- +goose Up

CREATE TABLE persona_run_policy (
    tenant_id             uuid        NOT NULL REFERENCES tenant (tenant_id),
    legal_entity_id       text        NOT NULL CHECK (btrim(legal_entity_id) <> ''),
    revision              bigint      NOT NULL CHECK (revision > 0),
    effective_from        timestamptz NOT NULL,
    effective_until       timestamptz,
    max_cost_micros       bigint      NOT NULL CHECK (max_cost_micros > 0),
    max_input_tokens      bigint      NOT NULL CHECK (max_input_tokens > 0),
    max_output_tokens     bigint      NOT NULL CHECK (max_output_tokens > 0),
    max_run_duration_ms   bigint      NOT NULL CHECK (max_run_duration_ms > 0),
    PRIMARY KEY (tenant_id, legal_entity_id, revision),
    CHECK (effective_until IS NULL OR effective_until > effective_from)
);
CREATE INDEX persona_run_policy_effective
    ON persona_run_policy (tenant_id, legal_entity_id, effective_from DESC);

CREATE TRIGGER persona_run_policy_immutable
    BEFORE UPDATE OR DELETE ON persona_run_policy
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE persona_run_policy ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_run_policy FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_run_policy
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- The serving role resolves policy only. Policy insertion belongs to the
-- separately credentialed authority role, never an invocation adapter.
GRANT SELECT ON persona_run_policy TO hcmnext_agent_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM persona_run_policy) THEN
        RAISE EXCEPTION 'cannot remove retained persona run policy';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE persona_run_policy;
