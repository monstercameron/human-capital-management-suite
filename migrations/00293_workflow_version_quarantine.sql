-- WF-RUN-009: governed quarantine of a bad compiled workflow version.
--
-- version.Quarantine flipped a version's status with no authority check, no
-- second approver and no rule for the instances already running on it, and
-- nothing in production called it. This append-only declaration log is the
-- governance record internal/data/workflowversionstore writes in the same
-- transaction as the status change:
--
--   QUARANTINE  declared by one principal and approved by a different one,
--               with a reason, an incident evidence reference and the
--               disposition every live instance takes at its next advancement:
--               PAUSE (paused at its next safe point), CONTINUE (runs to
--               completion) or BLOCK (refused until the quarantine is lifted).
--   LIFT        a reviewer who did not declare the quarantine, with the
--               validation evidence that justifies returning to service.
--
-- New starts need an ACTIVE version, so a quarantined one refuses them already.

-- +goose Up

CREATE TABLE workflow_version_quarantine (
    declaration_id       uuid        PRIMARY KEY,
    compiled_plan_digest text        NOT NULL REFERENCES workflow_compiled_version (compiled_plan_digest),
    action               text        NOT NULL,
    live_instance_policy text,
    reason               text        NOT NULL,
    evidence_ref         text        NOT NULL,
    declared_by          text        NOT NULL,
    approved_by          text        NOT NULL,
    authority            text        NOT NULL,
    recorded_at          timestamptz NOT NULL,
    CONSTRAINT workflow_version_quarantine_action CHECK (action IN ('QUARANTINE', 'LIFT')),
    CONSTRAINT workflow_version_quarantine_policy CHECK (
        (action = 'QUARANTINE' AND live_instance_policy IN ('PAUSE', 'CONTINUE', 'BLOCK'))
        OR (action = 'LIFT' AND live_instance_policy IS NULL)
    ),
    CONSTRAINT workflow_version_quarantine_distinct_approver CHECK (declared_by <> approved_by),
    CONSTRAINT workflow_version_quarantine_evidence CHECK (reason <> '' AND evidence_ref <> '')
);

CREATE INDEX workflow_version_quarantine_by_digest
    ON workflow_version_quarantine (compiled_plan_digest, recorded_at);

CREATE TRIGGER workflow_version_quarantine_append_only
    BEFORE UPDATE OR DELETE ON workflow_version_quarantine
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

GRANT SELECT, INSERT ON workflow_version_quarantine TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00293 is irreversible: migrations 00279-00292 already broke the rollback chain, so this migration keeps that true rather than claiming a reversibility no rollback can ever reach'; END $$;
-- +goose StatementEnd
