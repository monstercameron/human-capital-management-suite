-- WF-RUN-010: the durable governed cancellation decision.
--
-- Every served cancel path (execute.Driver.Cancel, the operator workflow
-- control, IntentService.CancelIntent on an executing promotion) builds its
-- cancellation facts from durable state -- node executions judged against the
-- pinned plan's declared cancellation semantics, recorded effect references,
-- child workflow instances -- runs workflow.DecideCancellation over them and
-- acts on the verdict in the same transaction that records one row here:
--
--   CANCELLED             the instance moved CANCELLING -> CANCELLED;
--   COMPENSATION_REQUIRED the instance moved to CANCELLING and the row carries
--                         the compensation obligation (compensation_refs);
--                         it is not declared cancelled;
--   CANNOT_CANCEL         the refusal and its reasons; the instance is
--                         untouched (status_before = status_after);
--   REPAIR_REQUIRED       the instance moved CANCELLING -> REPAIR_REQUIRED.
--
-- evidence is the decision's phase/effect evidence (workflow
-- .CancellationOutcome: effect dispositions, child reports, the verbatim child
-- history and its digest). The (tenant, instance, instance_version_before)
-- uniqueness is the convergence fence: one decision per observed instance
-- state, so concurrent cancels of the same state resolve to one decision.
-- parent_decision_id names the parent decision that propagated a child's
-- cancellation. Append-only: SELECT and INSERT are granted, UPDATE and DELETE
-- are refused by the forbid_mutation trigger; history is never deleted.

-- +goose Up

CREATE TABLE workflow_cancellation_decision (
    tenant_id               tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    decision_id             uuid           NOT NULL,
    instance_id             uuid           NOT NULL,
    parent_decision_id      uuid,
    decision                text           NOT NULL,
    phase                   text           NOT NULL,
    compiled_plan_hash      content_digest NOT NULL,
    status_before           text           NOT NULL,
    status_after            text           NOT NULL,
    instance_version_before cas_version    NOT NULL,
    instance_version_after  cas_version    NOT NULL,
    evidence                jsonb          NOT NULL,
    reasons                 text[]         NOT NULL DEFAULT '{}',
    compensation_refs       text[]         NOT NULL DEFAULT '{}',
    reason_ref              text           NOT NULL,
    requested_by            text           NOT NULL,
    outcome_digest          text           NOT NULL,
    decided_at              timestamptz    NOT NULL,
    recorded_at             timestamptz    NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, decision_id),
    CONSTRAINT workflow_cancellation_decision_state_unique
        UNIQUE (tenant_id, instance_id, instance_version_before),
    FOREIGN KEY (tenant_id, instance_id) REFERENCES workflow_instance (tenant_id, instance_id),
    CONSTRAINT workflow_cancellation_decision_allowed CHECK (
        decision IN ('CANCELLED', 'COMPENSATION_REQUIRED', 'CANNOT_CANCEL', 'REPAIR_REQUIRED')
    ),
    CONSTRAINT workflow_cancellation_decision_evidence_object CHECK (jsonb_typeof(evidence) = 'object'),
    CONSTRAINT workflow_cancellation_decision_version_order CHECK (instance_version_after >= instance_version_before),
    CONSTRAINT workflow_cancellation_decision_refusal_untouched CHECK (
        decision <> 'CANNOT_CANCEL'
        OR (status_after = status_before AND instance_version_after = instance_version_before)
    ),
    CONSTRAINT workflow_cancellation_decision_compensation_named CHECK (
        decision <> 'COMPENSATION_REQUIRED' OR cardinality(compensation_refs) > 0
    ),
    CONSTRAINT workflow_cancellation_decision_actor_present CHECK (
        length(btrim(requested_by)) > 0 AND length(btrim(reason_ref)) > 0
    )
);

CREATE INDEX workflow_cancellation_decision_instance
    ON workflow_cancellation_decision (tenant_id, instance_id, instance_version_after);

ALTER TABLE workflow_cancellation_decision ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_cancellation_decision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workflow_cancellation_decision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON workflow_cancellation_decision TO hcmnext_app;

CREATE TRIGGER workflow_cancellation_decision_forbid_mutation
    BEFORE UPDATE OR DELETE ON workflow_cancellation_decision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00304 is irreversible: migrations 00279-00302 already broke the rollback chain, so this migration keeps that true rather than claiming a reversibility no rollback can ever reach'; END $$;
-- +goose StatementEnd
