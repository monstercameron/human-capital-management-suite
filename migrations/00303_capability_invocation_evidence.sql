-- WF-RUN-035: durable execution and capability evidence.
--
-- Every capability-gateway decision (an invocation or a refusal, CAP-002),
-- every execution-authority gate decision and every OBS-024 execution-evidence
-- entry a workflow driver records (APPROVAL_COMPLETED, TASK_SUBMITTED,
-- TERMINAL_WRITTEN) was held only in an in-process sink, so a restart lost
-- the whole evidence chronology a journey's Inspect reads and an auditor
-- relies on. internal/data/evidencestore is the durable sink over this table.
--
-- Shape. One row per (tenant, evidence id). The tenant is never inferred: a
-- capability decision carries the tenant key its verified principal (or the
-- pinned workflow delegation) named, and an execution entry the storage
-- tenant its run committed under. record_digest is the sha256 of the row's
-- canonical content and evidence_id is derived from it, so re-recording the
-- same decision is an idempotent replay of the same row, never a second one,
-- and a reader re-derives the digest to detect a tampered row. record_seq
-- orders a tenant's chronology. The table is append-only: SELECT and INSERT
-- are granted, UPDATE and DELETE are refused by the forbid_mutation trigger.

-- +goose Up

CREATE TABLE capability_invocation_evidence (
    tenant_id          tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    evidence_id        text        NOT NULL,
    record_seq         bigint      GENERATED ALWAYS AS IDENTITY,
    capability_id      text        NOT NULL,
    capability_version bigint      NOT NULL,
    subject_ref        text        NOT NULL,
    decision           text        NOT NULL,
    reason_code        text        NOT NULL,
    occurred_at        timestamptz NOT NULL,
    purpose            text        NOT NULL,
    idempotency_key    text        NOT NULL,
    deadline           timestamptz,
    effect_class       text        NOT NULL,
    record_digest      text        NOT NULL,
    recorded_at        timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, evidence_id),
    CONSTRAINT capability_invocation_evidence_seq_unique UNIQUE (record_seq),
    CONSTRAINT capability_invocation_evidence_capability_present CHECK (capability_id <> '' AND capability_version > 0),
    CONSTRAINT capability_invocation_evidence_decision_present CHECK (decision <> ''),
    CONSTRAINT capability_invocation_evidence_digest_shape CHECK (record_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT capability_invocation_evidence_id_bound CHECK (evidence_id = 'ev:capability:' || substr(record_digest, 8, 24))
);

CREATE INDEX capability_invocation_evidence_subject
    ON capability_invocation_evidence (tenant_id, subject_ref, record_seq);

ALTER TABLE capability_invocation_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE capability_invocation_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON capability_invocation_evidence
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON capability_invocation_evidence TO hcmnext_app;

CREATE TRIGGER capability_invocation_evidence_forbid_mutation
    BEFORE UPDATE OR DELETE ON capability_invocation_evidence
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00303 is irreversible: migrations 00279-00302 already broke the rollback chain, so this migration keeps that true rather than claiming a reversibility no rollback can ever reach'; END $$;
-- +goose StatementEnd
