-- WF-EXT-004: the durable data plane a mapping actually reads from on the
-- EXECUTE path -- the run's typed input document, recorded once at start, and
-- each node's typed output, recorded once per attempt. Until this migration
-- neither existed durably: [frontier.NodeOutcome] carried only an output
-- digest and [Instance.InputRef] only a digest of the input, so nothing but
-- Promotion (which rebuilds its own inputs from the pinned proposal) could
-- run in EXECUTE. A DECISION or TRANSFORM node whose compiled mapping reads
-- workflow input, a predecessor's output or pinned context now resolves that
-- mapping against these two tables instead.
--
-- internal/workflow/runtime.RecordWorkflowInputs inserts one row per instance,
-- inside the same transaction as runtime.Start, only when the start created
-- the instance (a replay under the same start idempotency key compares its
-- document against the recorded one and is refused rather than silently
-- re-recording). RecordNodeOutputs inserts one row per node attempt, inside
-- the advancement transaction, exactly like migration 00296's
-- workflow_node_input_artifact. Both are read back with their digest
-- re-derived, and both are append-only for the same reason 00296 is: an
-- artifact a replay or an audit relies on must never change out from under it.

-- +goose Up

CREATE TABLE workflow_input_artifact (
    tenant_id    tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    instance_id  uuid        NOT NULL,
    plan_digest  text        NOT NULL,
    input_digest text        NOT NULL,
    artifact     jsonb       NOT NULL,
    recorded_at  timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, instance_id),
    FOREIGN KEY (tenant_id, instance_id) REFERENCES workflow_instance (tenant_id, instance_id),
    CONSTRAINT workflow_input_artifact_plan_present CHECK (plan_digest <> ''),
    CONSTRAINT workflow_input_artifact_digest_present CHECK (input_digest LIKE 'sha256:%')
);

CREATE TRIGGER workflow_input_artifact_append_only
    BEFORE UPDATE OR DELETE ON workflow_input_artifact
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE workflow_input_artifact ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_input_artifact FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workflow_input_artifact
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

REVOKE UPDATE, DELETE ON workflow_input_artifact FROM PUBLIC;
GRANT SELECT, INSERT ON workflow_input_artifact TO hcmnext_app;

CREATE TABLE workflow_node_output_artifact (
    tenant_id     tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    instance_id   uuid        NOT NULL,
    node_id       text        NOT NULL,
    attempt       integer     NOT NULL,
    plan_digest   text        NOT NULL,
    output_digest text        NOT NULL,
    artifact      jsonb       NOT NULL,
    recorded_at   timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, instance_id, node_id, attempt),
    FOREIGN KEY (tenant_id, instance_id) REFERENCES workflow_instance (tenant_id, instance_id),
    CONSTRAINT workflow_node_output_artifact_attempt_positive CHECK (attempt > 0),
    CONSTRAINT workflow_node_output_artifact_node_present CHECK (node_id <> ''),
    CONSTRAINT workflow_node_output_artifact_plan_present CHECK (plan_digest <> ''),
    CONSTRAINT workflow_node_output_artifact_digest_present CHECK (output_digest LIKE 'sha256:%')
);

CREATE TRIGGER workflow_node_output_artifact_append_only
    BEFORE UPDATE OR DELETE ON workflow_node_output_artifact
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE workflow_node_output_artifact ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_node_output_artifact FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workflow_node_output_artifact
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

REVOKE UPDATE, DELETE ON workflow_node_output_artifact FROM PUBLIC;
GRANT SELECT, INSERT ON workflow_node_output_artifact TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00322 is irreversible: migrations 00279-00292 already broke the rollback chain, so this migration keeps that true rather than claiming a reversibility no rollback can ever reach'; END $$;
-- +goose StatementEnd
