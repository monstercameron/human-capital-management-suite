-- WF-RUN-016: make the RepairPlan execution mode's idempotency record durable.
--
-- Why this table exists. internal/workflow/execute.RepairExecutor already
-- refuses to redrive a failed effect twice, but it remembered that refusal in
-- a process-local map. A cell that restarted between the corrective effect and
-- its reconciliation forgot the effect had ever run, and the next operator
-- submitting the same plan would redrive a payroll or IAM mutation that had
-- already been accepted by the external system. A repair exists precisely
-- because consistency is already degraded; duplicating its effect is the one
-- failure mode it must never add.
--
-- The record is append-only on purpose. Three stages are written, each exactly
-- once per tenant-scoped repair fence, and none of them is ever rewritten:
--
--   CLAIMED   written BEFORE the corrective effect is invoked. Its presence is
--             the durable statement "an attempt reached the effect boundary
--             under this fence". A process that dies immediately afterwards
--             leaves only this row, and the executor then refuses to re-run
--             the effect: the prior attempt's outcome is unknown, and an
--             unknown external mutation is diagnosed again, never repeated.
--   EXECUTED  written after the provider accepted the redrive, carrying the
--             effect identities so a restart can resume at observe/verify
--             without touching the external system a second time.
--   SETTLED   the terminal revalidation answer, so a replay of the same plan
--             returns the original decision instead of re-deciding it.
--
-- The primary key (tenant_id, fence_key, stage) is what makes the claim a
-- decision rather than a check: the executor issues one
-- INSERT .. ON CONFLICT DO NOTHING .. RETURNING, so two cells racing for the
-- same fence serialize on this table's own commit and exactly one of them
-- reaches the effect port. It is never a SELECT followed by a conditional
-- INSERT, for the same reason migrations/00286, 00287 and 00288 are not.
--
-- The fence key is the repair's own identity ("repair:<plan id>:<failed effect
-- key>", minted by internal/operations/repair), never the parent transaction's
-- semantic key: a repair is separately fenced from the business transaction it
-- corrects, and original_semantic_key is carried only as evidence of which
-- provider identity the redrive reused.

-- +goose Up

CREATE TABLE workflow_repair_execution_record (
    tenant_id             tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    -- The separately fenced repair identity; stable across restarts because it
    -- is derived from the immutable plan, not from a process.
    fence_key             semantic_key NOT NULL,
    stage                 text         NOT NULL,
    fence_id              semantic_key NOT NULL,
    plan_digest           semantic_key NOT NULL,
    -- The parent transaction's provider idempotency identity, reused by the
    -- redrive so the external system can deduplicate it on its own terms.
    original_semantic_key semantic_key NOT NULL,
    failed_effect_key     semantic_key NOT NULL,
    -- The typed revalidation answer this stage recorded (execute.RepairStatus).
    status                text         NOT NULL,
    executed              boolean      NOT NULL,
    consistency_state     text         NOT NULL,
    -- Effect, observation and reconciliation identities only: references,
    -- states and digests, never a business payload or a provider response body.
    effect_ref            text         NOT NULL DEFAULT '',
    effect_result_ref     text         NOT NULL DEFAULT '',
    observation_state     text         NOT NULL DEFAULT '',
    observation_digest    text         NOT NULL DEFAULT '',
    observation_complete  boolean      NOT NULL DEFAULT false,
    reconciliation_status text         NOT NULL DEFAULT '',
    reconciliation_route  text         NOT NULL DEFAULT '',
    recorded_at           timestamptz  NOT NULL,
    PRIMARY KEY (tenant_id, fence_key, stage),
    CONSTRAINT workflow_repair_execution_record_stage_allowed
        CHECK (stage IN ('CLAIMED', 'EXECUTED', 'SETTLED')),
    CONSTRAINT workflow_repair_execution_record_status_present
        CHECK (length(status) > 0),
    CONSTRAINT workflow_repair_execution_record_consistency_allowed
        CHECK (consistency_state IN ('CONSISTENT', 'DEGRADED', 'UNKNOWN'))
);

-- Reading one repair's history is always tenant-scoped and fence-scoped; the
-- primary key already serves that. This index serves the operator question
-- "which repairs did this cell claim and never settle", which is the backlog a
-- restart leaves behind.
CREATE INDEX workflow_repair_execution_record_open
    ON workflow_repair_execution_record (tenant_id, recorded_at)
    WHERE stage <> 'SETTLED';

CREATE TRIGGER workflow_repair_execution_record_append_only
    BEFORE UPDATE OR DELETE ON workflow_repair_execution_record
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE workflow_repair_execution_record ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_repair_execution_record FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workflow_repair_execution_record
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

REVOKE UPDATE, DELETE ON workflow_repair_execution_record FROM PUBLIC;

-- Append-only: SELECT and INSERT only. UPDATE and DELETE are never granted,
-- and the trigger above refuses them even to a role that acquired them.
GRANT SELECT, INSERT ON workflow_repair_execution_record TO hcmnext_app;

-- +goose Down
-- This record is the only durable proof that a corrective external effect
-- already ran. Dropping it would make a redrive that has already mutated a
-- payroll or IAM system look like one that never happened, which is exactly
-- the duplication this migration exists to prevent. It is also below the
-- irreversible chain migrations/00279 onwards already established.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00311 is irreversible: workflow_repair_execution_record is the durable proof that a corrective external effect already ran, and migrations 00279-00310 already broke the rollback chain'; END $$;
-- +goose StatementEnd
