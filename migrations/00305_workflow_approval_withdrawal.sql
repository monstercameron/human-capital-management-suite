-- WF-STEP-018: durable withdrawal of pending approval decisions.
--
-- A multi-approver requirement holds its decisions durably while it waits for
-- quorum: each vote completes its own work item and records its immutable
-- work_item_decision row (00022). When a declared invalidator fires before
-- quorum -- a material proposal change, revoked authority, an expired
-- delegation -- those pending votes must stop counting, but the decision rows
-- are append-only evidence and cannot be edited or deleted.
--
-- workflow_approval_withdrawal records the withdrawal instead: one row per
-- withdrawn decision, keyed by (tenant, work item), naming the decision digest
-- it withdraws, the invalidator kind and rule that fired, the reason and the
-- evidence of the change, the continuation the vote was pending against, and
-- who withdrew it when. The approval kernel inserts it in the same
-- transaction that cancels the requirement's still-open slots and routes the
-- APPROVAL node INVALIDATED, so the withdrawal and the route commit together
-- or not at all.
--
-- It is append-only: SELECT and INSERT are granted, never UPDATE or DELETE,
-- and forbid_mutation refuses both even for a privileged role.

-- +goose Up

CREATE TABLE workflow_approval_withdrawal (
    tenant_id            tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    work_item_id         uuid        NOT NULL,
    workflow_instance_id uuid        NOT NULL,
    node_id              text        NOT NULL,
    requirement_id       text        NOT NULL,
    decision_body_digest text        NOT NULL,
    decided_by           text        NOT NULL,
    invalidator_kind     text        NOT NULL,
    invalidator_rule_id  text        NOT NULL,
    reason               text        NOT NULL,
    evidence_ref         text        NOT NULL,
    continuation_digest  text        NOT NULL,
    withdrawn_by         text        NOT NULL,
    withdrawn_at         timestamptz NOT NULL,
    recorded_at          timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, work_item_id),
    FOREIGN KEY (tenant_id, work_item_id) REFERENCES work_item_decision (tenant_id, work_item_id),
    FOREIGN KEY (tenant_id, workflow_instance_id) REFERENCES workflow_instance (tenant_id, instance_id),
    CONSTRAINT workflow_approval_withdrawal_kind CHECK (invalidator_kind IN (
        'MATERIAL_PROPOSAL_CHANGE', 'AUTHORITY_REVOKED', 'DELEGATION_EXPIRED', 'DEADLINE_EXPIRED', 'MANDATORY_DENY')),
    CONSTRAINT workflow_approval_withdrawal_identity CHECK (
        node_id <> '' AND requirement_id <> '' AND decided_by <> '' AND withdrawn_by <> ''
        AND invalidator_rule_id <> '' AND reason <> '' AND evidence_ref <> '' AND continuation_digest <> ''),
    CONSTRAINT workflow_approval_withdrawal_digest CHECK (decision_body_digest ~ '^sha256:[0-9a-f]{64}$')
);

CREATE INDEX workflow_approval_withdrawal_by_instance
    ON workflow_approval_withdrawal (tenant_id, workflow_instance_id);

CREATE TRIGGER workflow_approval_withdrawal_append_only
    BEFORE UPDATE OR DELETE ON workflow_approval_withdrawal
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE workflow_approval_withdrawal ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_approval_withdrawal FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workflow_approval_withdrawal
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON workflow_approval_withdrawal TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00305 is irreversible: migrations 00279-00302 already broke the rollback chain, so this migration keeps that true rather than claiming a reversibility no rollback can ever reach'; END $$;
-- +goose StatementEnd
