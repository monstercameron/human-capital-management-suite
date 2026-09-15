-- WF-COMP-007: append-only WorkflowVariableRevision records.
--
-- specs/workflow-runtime.md says shared values that genuinely evolve use
-- explicit WorkflowVariableRevision records with writer, reason, schema and
-- causation. Until this migration workflow_instance.variable_revision_head
-- (00016) counted revisions nobody held, and workflow_variable (00026) kept
-- only the current value: an overwrite destroyed the prior value, its writer
-- and the reason it changed.
--
-- runtime.Store.AppendVariableRevision inserts exactly one row here per
-- variable write, advances workflow_instance.variable_revision_head from
-- revision - 1 to revision under the instance-version compare-and-set, and
-- refreshes the workflow_variable projection, all in the caller's
-- transaction. Revisions are dense per instance and hash-chained: each row
-- carries the digest of the instance's previous revision, so a gap, a reorder
-- or an edited row fails runtime.NewVariableRevisionLog on read.
--
-- The table is append-only: SELECT and INSERT are granted, never UPDATE or
-- DELETE, and forbid_mutation refuses both even to a privileged role.

-- +goose Up

CREATE TABLE workflow_variable_revision (
    tenant_id          tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    instance_id        uuid         NOT NULL,
    revision           bigint       NOT NULL,
    variable_name      semantic_key NOT NULL,
    schema_ref         semantic_key NOT NULL,
    variable_value     jsonb        NOT NULL,
    value_digest       text         NOT NULL,
    writer_node_id     semantic_key NOT NULL,
    writer_ref         semantic_key NOT NULL,
    reason             text         NOT NULL,
    causation_id       semantic_key NOT NULL,
    -- The previous revision of the same variable, 0 for its first write.
    previous_revision  bigint       NOT NULL,
    -- The revision digest of this instance's revision - 1, '' for revision 1.
    prior_digest       text         NOT NULL,
    revision_digest    text         NOT NULL,
    written_at         timestamptz  NOT NULL,
    recorded_at        timestamptz  NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, instance_id, revision),
    FOREIGN KEY (tenant_id, instance_id) REFERENCES workflow_instance (tenant_id, instance_id),
    CONSTRAINT workflow_variable_revision_positive CHECK (revision >= 1),
    CONSTRAINT workflow_variable_revision_previous_before CHECK (
        previous_revision >= 0 AND previous_revision < revision
    ),
    CONSTRAINT workflow_variable_revision_chain_start CHECK (
        (revision = 1) = (prior_digest = '')
    ),
    CONSTRAINT workflow_variable_revision_value_object CHECK (jsonb_typeof(variable_value) = 'object'),
    CONSTRAINT workflow_variable_revision_reason_present CHECK (btrim(reason) <> ''),
    CONSTRAINT workflow_variable_revision_digests CHECK (
        value_digest LIKE 'sha256:%' AND revision_digest LIKE 'sha256:%'
    )
);

CREATE INDEX workflow_variable_revision_by_variable
    ON workflow_variable_revision (tenant_id, instance_id, variable_name, revision);

CREATE TRIGGER workflow_variable_revision_append_only
    BEFORE UPDATE OR DELETE ON workflow_variable_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE workflow_variable_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_variable_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workflow_variable_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON workflow_variable_revision TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00295 is irreversible: migrations 00279-00294 already broke the rollback chain, so this migration keeps that true rather than claiming a reversibility no rollback can ever reach'; END $$;
-- +goose StatementEnd
