-- WF-RUN-015: immutable workflow intervention decisions.
--
-- A governed intervention (retry, resume, skip, satisfy, override, rewind,
-- supersede, reconcile, cancel -- internal/workflow/intervention) reaches the runtime
-- only through the operator gateway (internal/intent/operator/workflowcontrol).
-- When it is accepted the executor performs the transition through the
-- runtime's own APIs and, in the same tenant transaction, records here the
-- decision: the kind and its capability family, the operator kind and
-- operational intent whose receipt operator_control_receipt journals, the
-- idempotency key, requester, reason and evidence, the evaluated plan and the
-- transition read back afterwards, sealed by a digest re-checked on read.
-- A denied or no-op intervention records nothing.
--
-- One decision per (tenant, instance, expected instance version): two
-- concurrent interventions against the same version converge to one row. The
-- table is append-only: SELECT and INSERT are granted, UPDATE and DELETE are
-- refused by the forbid_mutation trigger.

-- +goose Up

CREATE TABLE workflow_intervention_decision (
    tenant_id                  tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    decision_id                uuid        NOT NULL,
    instance_id                uuid        NOT NULL,
    kind                       text        NOT NULL,
    capability                 text        NOT NULL,
    operator_kind              text        NOT NULL,
    intent_instance_id         text        NOT NULL,
    idempotency_key            text        NOT NULL,
    requested_by               text        NOT NULL,
    reason                     text        NOT NULL,
    evidence_refs              text[]      NOT NULL,
    expected_instance_version  bigint      NOT NULL,
    resulting_instance_version bigint      NOT NULL,
    instance_from              text        NOT NULL,
    instance_to                text        NOT NULL,
    node_id                    text,
    plan                       jsonb       NOT NULL,
    observed                   jsonb       NOT NULL,
    decided_at                 timestamptz NOT NULL,
    decision_digest            text        NOT NULL,
    PRIMARY KEY (tenant_id, decision_id),
    FOREIGN KEY (tenant_id, instance_id) REFERENCES workflow_instance (tenant_id, instance_id),
    CONSTRAINT workflow_intervention_decision_one_per_version UNIQUE (tenant_id, instance_id, expected_instance_version),
    CONSTRAINT workflow_intervention_decision_one_per_key UNIQUE (tenant_id, idempotency_key),
    CONSTRAINT workflow_intervention_decision_kind CHECK (kind IN
        ('RETRY', 'RESUME', 'SKIP', 'SATISFY', 'OVERRIDE', 'REWIND', 'COMPENSATE', 'SUPERSEDE', 'RECONCILE', 'CANCEL')),
    CONSTRAINT workflow_intervention_decision_reason_present CHECK (length(btrim(reason)) > 0),
    CONSTRAINT workflow_intervention_decision_evidence_present CHECK (cardinality(evidence_refs) > 0),
    CONSTRAINT workflow_intervention_decision_authority_present CHECK (
        length(btrim(requested_by)) > 0 AND operator_kind <> '' AND intent_instance_id <> '' AND idempotency_key <> ''),
    CONSTRAINT workflow_intervention_decision_transition_observed CHECK (resulting_instance_version > expected_instance_version),
    CONSTRAINT workflow_intervention_decision_digest_present CHECK (decision_digest <> '')
);

ALTER TABLE workflow_intervention_decision ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_intervention_decision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workflow_intervention_decision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON workflow_intervention_decision TO hcmnext_app;

CREATE TRIGGER workflow_intervention_decision_forbid_mutation
    BEFORE UPDATE OR DELETE ON workflow_intervention_decision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00307 is irreversible: migrations 00279-00302 already broke the rollback chain, so this migration keeps that true rather than claiming a reversibility no rollback can ever reach'; END $$;
-- +goose StatementEnd
