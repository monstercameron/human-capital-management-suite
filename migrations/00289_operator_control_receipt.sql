-- EP-WF-002 / WF-RUN-035: durable operator control receipts.
--
-- The governed operator gateway (internal/intent/operator.Gateway) records a
-- PENDING receipt before an intervention executes and replaces it with the
-- final, digest-sealed receipt afterwards; a repeated request under the same
-- idempotency key replays the recorded receipt and never executes twice.
-- Until this table the only Journal was an in-process map, so a restart lost
-- every control receipt: a replayed Cancel or RetryNode would execute again
-- and the intervention audit trail vanished. internal/data/operatorjournal is
-- the durable Journal over this table.
--
-- Shape. One row per (tenant, idempotency key). The full sealed receipt is
-- kept verbatim as JSON; outcome and request_digest are lifted into columns
-- so Complete and Abort can fence on them. Abort does not delete: the data
-- plane grants no DELETE (DB-017), so an aborted pending receipt is marked
-- ABORTED and a later Begin for the same key may reuse the row.

-- +goose Up

CREATE TABLE operator_control_receipt (
    tenant_id       tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    idempotency_key text        NOT NULL,
    kind            text        NOT NULL,
    outcome         text        NOT NULL,
    request_digest  text        NOT NULL,
    receipt         jsonb       NOT NULL,
    recorded_at     timestamptz NOT NULL,
    updated_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, idempotency_key),
    CONSTRAINT operator_control_receipt_key_bounded CHECK (idempotency_key <> '' AND length(idempotency_key) <= 512),
    CONSTRAINT operator_control_receipt_kind_present CHECK (kind <> ''),
    CONSTRAINT operator_control_receipt_outcome_present CHECK (outcome <> ''),
    CONSTRAINT operator_control_receipt_digest_present CHECK (request_digest <> '')
);

ALTER TABLE operator_control_receipt ENABLE ROW LEVEL SECURITY;
ALTER TABLE operator_control_receipt FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON operator_control_receipt
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- Receipts move PENDING -> final (or PENDING -> ABORTED -> PENDING on a
-- reused key): UPDATE is granted, DELETE is not.
GRANT SELECT, INSERT, UPDATE ON operator_control_receipt TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00289 is irreversible: migrations 00279-00288 already broke the rollback chain, so this migration keeps that true rather than claiming a reversibility no rollback can ever reach'; END $$;
-- +goose StatementEnd
