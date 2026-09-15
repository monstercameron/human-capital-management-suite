-- WF-RUN-013: the pinned inputs a pure workflow node was evaluated against.
--
-- A deterministic REPLAY recomputes a pure node (a DECISION's rule table, a
-- TRANSFORM, a read-only capability whose inputs are pinned) with a candidate
-- implementation and compares the result with what the historical run
-- recorded. Until this table nothing durable held those inputs: the node
-- execution row carries only the output artifact digest, and the advancement
-- receipt only a digest of the outcome, so a replay could either copy the
-- recorded route key or read current, mutable domain data. Both are wrong.
--
-- internal/workflow/runtime.RecordNodeInputs inserts one row per node attempt
-- inside the advancement transaction (a TransactionalStepRunner's RunInTx):
-- the typed input values, the versions the evaluation pinned (a rule table's
-- id, version and digest), the plan digest and the execution-context digest
-- the instance pinned at start, and the canonical digest of all of it.
-- runtime.LoadNodeInputs re-derives every row's digest on read and refuses a
-- row whose content no longer matches. Rows are historical evidence:
-- append-only, SELECT and INSERT granted, never UPDATE or DELETE.

-- +goose Up

CREATE TABLE workflow_node_input_artifact (
    tenant_id      tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    instance_id    uuid        NOT NULL,
    node_id        text        NOT NULL,
    attempt        integer     NOT NULL,
    plan_digest    text        NOT NULL,
    context_digest text        NOT NULL DEFAULT '',
    input_digest   text        NOT NULL,
    artifact       jsonb       NOT NULL,
    recorded_at    timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, instance_id, node_id, attempt),
    FOREIGN KEY (tenant_id, instance_id) REFERENCES workflow_instance (tenant_id, instance_id),
    CONSTRAINT workflow_node_input_artifact_attempt_positive CHECK (attempt > 0),
    CONSTRAINT workflow_node_input_artifact_node_present CHECK (node_id <> ''),
    CONSTRAINT workflow_node_input_artifact_plan_present CHECK (plan_digest <> ''),
    CONSTRAINT workflow_node_input_artifact_digest_present CHECK (input_digest LIKE 'sha256:%')
);

CREATE TRIGGER workflow_node_input_artifact_append_only
    BEFORE UPDATE OR DELETE ON workflow_node_input_artifact
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE workflow_node_input_artifact ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_node_input_artifact FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workflow_node_input_artifact
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

REVOKE UPDATE, DELETE ON workflow_node_input_artifact FROM PUBLIC;
GRANT SELECT, INSERT ON workflow_node_input_artifact TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00296 is irreversible: migrations 00279-00292 already broke the rollback chain, so this migration keeps that true rather than claiming a reversibility no rollback can ever reach'; END $$;
-- +goose StatementEnd
