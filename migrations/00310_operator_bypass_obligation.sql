-- WF-RUN-039: durable bypass obligations for the governed operator gateway.
--
-- A break-glass or bypassing operator action skips the dual control and the
-- simulation its kind's policy demands. Until this table the only record of
-- that was the receipt's review_required flag: nothing said by when the review
-- was owed, nothing suspended the authority when it was not done, and a
-- restart lost the fact entirely because the obligation lived only in the
-- gateway's memory.
--
-- operator_bypass_obligation records the debt next to the receipt that
-- incurred it (00289 operator_control_receipt), so obligations reuse the
-- operator gateway journal rather than opening a second store. One row per
-- (tenant, obligation id), where the obligation id is derived from the same
-- tenant-scoped idempotency key as the receipt, so re-recording one bypass is
-- the same obligation.
--
-- Shape. The sealed obligation is kept verbatim as JSON; the columns lifted
-- out of it are the ones the gateway queries or fences on: the family a
-- suspension is measured in, the operator who acted and the approver of record
-- (the repair-separation rule reads approver_ref), the due review instant, and
-- the review that discharges it. An obligation is never deleted and never
-- rewritten: the only UPDATE the data plane can make is the one that fills in
-- a review of a still-outstanding row, and DELETE is not granted at all.

-- +goose Up

CREATE TABLE operator_bypass_obligation (
    tenant_id       tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    obligation_id   text        NOT NULL,
    idempotency_key text        NOT NULL,
    kind            text        NOT NULL,
    family          text        NOT NULL,
    operator_ref    text        NOT NULL,
    approver_ref    text        NOT NULL,
    obligation      jsonb       NOT NULL,
    recorded_at     timestamptz NOT NULL,
    due_at          timestamptz NOT NULL,
    reviewed_at     timestamptz,
    review_outcome  text,
    reviewer_ref    text,
    updated_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, obligation_id),
    FOREIGN KEY (tenant_id, idempotency_key) REFERENCES operator_control_receipt (tenant_id, idempotency_key),
    CONSTRAINT operator_bypass_obligation_identity CHECK (
        obligation_id <> '' AND idempotency_key <> '' AND kind <> '' AND family <> ''
        AND operator_ref <> '' AND approver_ref <> ''),
    CONSTRAINT operator_bypass_obligation_key_bounded CHECK (length(obligation_id) <= 512 AND length(idempotency_key) <= 512),
    CONSTRAINT operator_bypass_obligation_due_after_record CHECK (due_at > recorded_at),
    CONSTRAINT operator_bypass_obligation_outcome CHECK (review_outcome IS NULL OR review_outcome IN ('JUSTIFIED', 'VIOLATION')),
    CONSTRAINT operator_bypass_obligation_review_complete CHECK (
        (reviewed_at IS NULL AND review_outcome IS NULL AND reviewer_ref IS NULL)
        OR (reviewed_at IS NOT NULL AND review_outcome IS NOT NULL AND reviewer_ref IS NOT NULL AND reviewer_ref <> '')),
    CONSTRAINT operator_bypass_obligation_reviewer_distinct CHECK (
        reviewer_ref IS NULL OR (lower(reviewer_ref) <> lower(operator_ref) AND lower(reviewer_ref) <> lower(approver_ref)))
);

-- The gateway's hot read: every undischarged obligation of one tenant, oldest
-- due date first.
CREATE INDEX operator_bypass_obligation_outstanding
    ON operator_bypass_obligation (tenant_id, due_at)
    WHERE reviewed_at IS NULL;

ALTER TABLE operator_bypass_obligation ENABLE ROW LEVEL SECURITY;
ALTER TABLE operator_bypass_obligation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON operator_bypass_obligation
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- An obligation is discharged by recording its review; nothing else about it
-- may change, and a discharged row is permanent evidence.
-- +goose StatementBegin
CREATE FUNCTION operator_bypass_obligation_review_only() RETURNS trigger AS $$
BEGIN
    IF OLD.reviewed_at IS NOT NULL THEN
        RAISE EXCEPTION 'operator_bypass_obligation %/% is already discharged and is immutable', OLD.tenant_id, OLD.obligation_id;
    END IF;
    IF NEW.tenant_id <> OLD.tenant_id OR NEW.obligation_id <> OLD.obligation_id
        OR NEW.idempotency_key <> OLD.idempotency_key OR NEW.kind <> OLD.kind OR NEW.family <> OLD.family
        OR NEW.operator_ref <> OLD.operator_ref OR NEW.approver_ref <> OLD.approver_ref
        OR NEW.recorded_at <> OLD.recorded_at OR NEW.due_at <> OLD.due_at THEN
        RAISE EXCEPTION 'operator_bypass_obligation %/% may only be updated to record its review', OLD.tenant_id, OLD.obligation_id;
    END IF;
    IF NEW.reviewed_at IS NULL THEN
        RAISE EXCEPTION 'operator_bypass_obligation %/% may only be updated to record its review', OLD.tenant_id, OLD.obligation_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER operator_bypass_obligation_review_only
    BEFORE UPDATE ON operator_bypass_obligation
    FOR EACH ROW EXECUTE FUNCTION operator_bypass_obligation_review_only();

CREATE TRIGGER operator_bypass_obligation_no_delete
    BEFORE DELETE ON operator_bypass_obligation
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

GRANT SELECT, INSERT, UPDATE ON operator_bypass_obligation TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00310 is irreversible: migrations 00279-00305 already broke the rollback chain, so this migration keeps that true rather than claiming a reversibility no rollback can ever reach'; END $$;
-- +goose StatementEnd
