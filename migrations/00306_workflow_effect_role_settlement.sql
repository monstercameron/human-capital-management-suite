-- WF-RUN-037: durable settlement of a failed downstream effect or derived
-- update against the authoritative core it follows.
--
-- specs/workflow-runtime.md "Authoritative core versus downstream effects"
-- classifies every write-effect node as AUTHORITATIVE_CORE, DOWNSTREAM_EFFECT
-- or DERIVED_UPDATE. Until this migration a failed downstream leg either
-- aborted the advance transaction (leaving the instance RUNNING with nothing to
-- run, the committed core outcome masked behind an error) or was refused
-- NO_FAILURE_ROUTE. Nothing durable said which committed core the failure
-- belonged to or where it was routed.
--
-- workflow_effect_role_settlement retains one sealed row per failed dependent
-- node attempt: its role, effect class and effect key, the route it took
-- (RECONCILIATION for a downstream effect, REBUILD_FROM_CORE for a derived
-- update), the compiled failure route the instance followed, the error class,
-- the pinned plan digest and the core node executions (id, execution id,
-- output artifact ref) that had already committed. The execute driver inserts
-- it in the same advance transaction that records the attempt FAILED and
-- moves the instance along its failure route, so the row and the route commit
-- together, and the core's own earlier commit is never touched.
--
-- It is append-only: SELECT and INSERT are granted, UPDATE and DELETE are
-- refused by forbid_mutation.

-- +goose Up

CREATE TABLE workflow_effect_role_settlement (
    tenant_id          tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    instance_id        uuid         NOT NULL,
    node_id            semantic_key NOT NULL,
    attempt            integer      NOT NULL,
    effect_role        text         NOT NULL,
    effect_class       text         NOT NULL,
    effect_key         text         NOT NULL,
    route              text         NOT NULL,
    failure_route      text         NOT NULL,
    error_class        text         NOT NULL,
    plan_digest        text         NOT NULL,
    core_node_ids      text[]       NOT NULL,
    core_execution_ids text[]       NOT NULL,
    core_output_refs   text[]       NOT NULL,
    record_digest      text         NOT NULL,
    recorded_at        timestamptz  NOT NULL,
    PRIMARY KEY (tenant_id, instance_id, node_id, attempt),
    FOREIGN KEY (tenant_id, instance_id) REFERENCES workflow_instance (tenant_id, instance_id),
    FOREIGN KEY (tenant_id, instance_id, node_id, attempt)
        REFERENCES workflow_node_execution (tenant_id, instance_id, node_id, attempt),
    CONSTRAINT workflow_effect_role_settlement_attempt_positive CHECK (attempt >= 1),
    CONSTRAINT workflow_effect_role_settlement_role CHECK (effect_role IN ('DOWNSTREAM_EFFECT', 'DERIVED_UPDATE')),
    CONSTRAINT workflow_effect_role_settlement_route CHECK (
        (effect_role = 'DOWNSTREAM_EFFECT' AND route = 'RECONCILIATION')
        OR (effect_role = 'DERIVED_UPDATE' AND route = 'REBUILD_FROM_CORE')
    ),
    CONSTRAINT workflow_effect_role_settlement_identity CHECK (
        failure_route <> '' AND error_class <> '' AND plan_digest <> '' AND effect_class <> ''
    ),
    CONSTRAINT workflow_effect_role_settlement_cores_aligned CHECK (
        cardinality(core_node_ids) = cardinality(core_execution_ids)
        AND cardinality(core_node_ids) = cardinality(core_output_refs)
    ),
    CONSTRAINT workflow_effect_role_settlement_rebuild_has_core CHECK (
        effect_role <> 'DERIVED_UPDATE' OR cardinality(core_node_ids) > 0
    ),
    CONSTRAINT workflow_effect_role_settlement_digest CHECK (record_digest LIKE 'sha256:%')
);

CREATE INDEX workflow_effect_role_settlement_by_route
    ON workflow_effect_role_settlement (tenant_id, route, recorded_at);

CREATE TRIGGER workflow_effect_role_settlement_append_only
    BEFORE UPDATE OR DELETE ON workflow_effect_role_settlement
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE workflow_effect_role_settlement ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_effect_role_settlement FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workflow_effect_role_settlement
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

REVOKE DELETE, UPDATE ON workflow_effect_role_settlement FROM PUBLIC;
GRANT SELECT, INSERT ON workflow_effect_role_settlement TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00306 is irreversible: migrations 00279-00302 already broke the rollback chain, so this migration keeps that true rather than claiming a reversibility no rollback can ever reach'; END $$;
-- +goose StatementEnd
