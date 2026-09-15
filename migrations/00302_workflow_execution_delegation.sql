-- WF-RUN-034: the authority a workflow instance's later steps act under.
--
-- A served promotion's steps run after the ExecuteIntent call that started
-- the instance has returned: on approval resumes, on the timer the scheduler
-- fires at the effective date, on retries. None of those has an authenticated
-- caller, yet every capability invocation must pass the capability gateway
-- with a principal and purpose. runtime.Start therefore records here, in the
-- start transaction, the verified principal that executed the proposal: its
-- subject, subject kind, tenant, organization scope, roles and purposes, the
-- authentication method, assurance and session it acted in and its
-- authentication evidence reference. Each step
-- loads the row and re-authorizes it against the current policy before every
-- invocation, so the row is an upper bound on authority, never a standing
-- grant: a revoked role fails the next step closed.
--
-- The row is deliberately not part of workflow_execution_context or its
-- digest (00291): resume paths rebuild the start request without it. It is
-- append-only: SELECT and INSERT are granted, UPDATE and DELETE are refused by
-- the forbid_mutation trigger.

-- +goose Up

CREATE TABLE workflow_execution_delegation (
    tenant_id             tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    instance_id           uuid        NOT NULL,
    subject               text        NOT NULL,
    subject_kind          text        NOT NULL,
    tenant_key            text        NOT NULL,
    organization_scope_id text        NOT NULL,
    roles                 text[]      NOT NULL,
    purposes              text[]      NOT NULL,
    authentication_method text        NOT NULL,
    assurance             text        NOT NULL,
    session_ref           text        NOT NULL,
    evidence_ref          text        NOT NULL,
    recorded_at           timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, instance_id),
    FOREIGN KEY (tenant_id, instance_id) REFERENCES workflow_instance (tenant_id, instance_id),
    CONSTRAINT workflow_execution_delegation_subject_present CHECK (length(btrim(subject)) > 0),
    CONSTRAINT workflow_execution_delegation_purpose_present CHECK (cardinality(purposes) > 0)
);

ALTER TABLE workflow_execution_delegation ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_execution_delegation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workflow_execution_delegation
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON workflow_execution_delegation TO hcmnext_app;

CREATE TRIGGER workflow_execution_delegation_forbid_mutation
    BEFORE UPDATE OR DELETE ON workflow_execution_delegation
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00302 is irreversible: migrations 00279-00298 already broke the rollback chain, so this migration keeps that true rather than claiming a reversibility no rollback can ever reach'; END $$;
-- +goose StatementEnd
