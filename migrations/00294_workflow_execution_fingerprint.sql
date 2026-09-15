-- WF-RUN-038: the version fingerprint every workflow node execution ran under,
-- and the tenant-scoped blast-radius index over it.
--
-- specs/workflow-runtime.md "Execution Fingerprint" requires every material
-- execution to record the workflow, runtime, schema, policy, mapping,
-- connector and reference-data versions it used, so operators can find every
-- execution a bad release touched. Until this table only the instance's start
-- digest (workflow_instance.input_ref) and its execution context existed; none
-- of the per-node versions were recorded or queryable.
--
-- runtime.Start and runtime.Advance derive each fingerprint with the pure
-- runtime.DeriveNodeFingerprint from the compiled plan the instance pinned by
-- digest plus the runtime version its execution context pinned -- never from
-- anything recomputed at run time -- and insert one row per node execution in
-- the same transaction that inserts the node execution itself.
--
--   workflow_id .. runtime_version  the scalar version components as named
--                                   columns (plan digest and runtime version
--                                   carry their own btree indexes).
--   components                      every component as a 'KIND=ref' token,
--                                   canonical order; the GIN index answers
--                                   runtime.InstancesTouching (components @>).
--   fingerprint_digest              content digest re-verified on every read.
--
-- The row is append-only: SELECT and INSERT are granted, UPDATE and DELETE are
-- forbidden by trigger.

-- +goose Up

CREATE TABLE workflow_execution_fingerprint (
    tenant_id            tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    instance_id          uuid         NOT NULL,
    node_id              semantic_key NOT NULL,
    attempt              integer      NOT NULL,
    step_type            semantic_key NOT NULL,
    workflow_id          text         NOT NULL,
    workflow_version     integer      NOT NULL,
    compiled_plan_digest text         NOT NULL,
    compiler_version     text         NOT NULL,
    runtime_version      text         NOT NULL,
    components           text[]       NOT NULL,
    fingerprint_digest   text         NOT NULL,
    recorded_at          timestamptz  NOT NULL,
    PRIMARY KEY (tenant_id, instance_id, node_id, attempt),
    FOREIGN KEY (tenant_id, instance_id) REFERENCES workflow_instance (tenant_id, instance_id),
    FOREIGN KEY (tenant_id, instance_id, node_id, attempt)
        REFERENCES workflow_node_execution (tenant_id, instance_id, node_id, attempt),
    CONSTRAINT workflow_execution_fingerprint_attempt_positive CHECK (attempt >= 1),
    CONSTRAINT workflow_execution_fingerprint_digest_present CHECK (fingerprint_digest LIKE 'sha256:%'),
    CONSTRAINT workflow_execution_fingerprint_versions_present CHECK (
        workflow_id <> '' AND compiled_plan_digest <> '' AND compiler_version <> '' AND runtime_version <> ''
    ),
    CONSTRAINT workflow_execution_fingerprint_components_present CHECK (cardinality(components) > 0)
);

CREATE INDEX workflow_execution_fingerprint_components
    ON workflow_execution_fingerprint USING gin (components);
CREATE INDEX workflow_execution_fingerprint_by_plan
    ON workflow_execution_fingerprint (tenant_id, compiled_plan_digest, instance_id);
CREATE INDEX workflow_execution_fingerprint_by_runtime
    ON workflow_execution_fingerprint (tenant_id, runtime_version, instance_id);

CREATE TRIGGER workflow_execution_fingerprint_append_only
    BEFORE UPDATE OR DELETE ON workflow_execution_fingerprint
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE workflow_execution_fingerprint ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_execution_fingerprint FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workflow_execution_fingerprint
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

REVOKE DELETE, UPDATE ON workflow_execution_fingerprint FROM PUBLIC;
GRANT SELECT, INSERT ON workflow_execution_fingerprint TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00294 is irreversible: migrations 00279-00292 already broke the rollback chain, so this migration keeps that true rather than claiming a reversibility no rollback can ever reach'; END $$;
-- +goose StatementEnd
