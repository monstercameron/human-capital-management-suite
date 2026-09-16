-- WF-RUN-034: the historical GOVERN-002 decision a promotion approval records.
--
-- GOVERN-003 revalidation (internal/governance/revalidate) can only confirm a
-- decision it can recompose: it needs the exact decision.Inputs GOVERN-002
-- composed at approval, the decision that composition produced, the seven
-- revalidated facts as the ports reported them then, and the plan digest the
-- decision authorized. Until this table the served path recorded none of it,
-- so a served promotion could never confirm at its effective date and closed
-- PROMOTION_BLOCKED.
--
-- One row is written per approval decision, in the approving request, before
-- the workflow is resumed: the work item the decision closed identifies it, so
-- a replayed decision writes nothing new. The record is append-only and
-- digest-bound -- revalidation recomposes the stored inputs and refuses a
-- record whose recomputed digest does not reproduce the stored one -- so a
-- tampered row fails closed rather than authorizing a commit.

-- +goose Up

CREATE TABLE promotion_approval_governance (
    tenant_id            tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    instance_id          uuid        NOT NULL,
    work_item_id         uuid        NOT NULL,
    node_id              text        NOT NULL,
    proposal_revision_id uuid        NOT NULL,
    material_digest      text        NOT NULL,
    plan_digest          text        NOT NULL,
    decision_state       text        NOT NULL,
    decision_digest      text        NOT NULL,
    record               jsonb       NOT NULL,
    recorded_at          timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, instance_id, work_item_id),
    FOREIGN KEY (tenant_id, instance_id) REFERENCES workflow_instance (tenant_id, instance_id),
    CONSTRAINT promotion_approval_governance_node_present CHECK (length(btrim(node_id)) > 0),
    CONSTRAINT promotion_approval_governance_digests_present CHECK (
        length(btrim(material_digest)) > 0 AND length(btrim(plan_digest)) > 0 AND length(btrim(decision_digest)) > 0
    )
);

CREATE INDEX promotion_approval_governance_instance
    ON promotion_approval_governance (tenant_id, instance_id, recorded_at DESC);

ALTER TABLE promotion_approval_governance ENABLE ROW LEVEL SECURITY;
ALTER TABLE promotion_approval_governance FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON promotion_approval_governance
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON promotion_approval_governance TO hcmnext_app;

CREATE TRIGGER promotion_approval_governance_forbid_mutation
    BEFORE UPDATE OR DELETE ON promotion_approval_governance
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00309 is irreversible: migrations 00279-00308 already broke the rollback chain, so this migration keeps that true rather than claiming a reversibility no rollback can ever reach'; END $$;
-- +goose StatementEnd
