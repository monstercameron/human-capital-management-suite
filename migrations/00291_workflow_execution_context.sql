-- WF-RUN-040: the immutable execution context a workflow instance runs under.
--
-- specs/workflow-runtime.md requires every node to execute inside one
-- WorkflowExecutionContext -- principal, tenant, organization, locale, legal,
-- entitlement, risk and billing context plus the execution mode, workflow
-- version and runtime version -- pinned when the instance starts. Until this
-- table nothing recorded it: workflow_instance.effective_context_ref was
-- always NULL and steps received no context at all.
--
-- runtime.Start derives the context from the approved proposal revision, the
-- pinned plan and the start request, inserts exactly one row here in the start
-- transaction, and stores its digest in workflow_instance.effective_context_ref.
-- runtime.LoadExecutionContext re-reads the row and refuses it unless its
-- digest still equals the instance's pin. The row is append-only: SELECT and
-- INSERT are granted, never UPDATE or DELETE.

-- +goose Up

CREATE TABLE workflow_execution_context (
    tenant_id      tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    instance_id    uuid        NOT NULL,
    context_digest text        NOT NULL,
    context        jsonb       NOT NULL,
    recorded_at    timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, instance_id),
    FOREIGN KEY (tenant_id, instance_id) REFERENCES workflow_instance (tenant_id, instance_id),
    CONSTRAINT workflow_execution_context_digest_present CHECK (context_digest LIKE 'sha256:%')
);

ALTER TABLE workflow_execution_context ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_execution_context FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workflow_execution_context
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON workflow_execution_context TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00291 is irreversible: migrations 00279-00290 already broke the rollback chain, so this migration keeps that true rather than claiming a reversibility no rollback can ever reach'; END $$;
-- +goose StatementEnd
