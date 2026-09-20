-- Integration provider receipts: the durable record of one signed RESULT
-- callback ("receipt") from a third-party provider (the payroll and IAM
-- simulators today), or of a delivery the provider refused outright.
--
-- A receipt is the provider's own statement of what became of one outbound
-- change: APPLIED (payroll) or GRANTED (IAM) when it took effect, REJECTED
-- when it did not, and REVERSED (payroll) or REVOKED (IAM) when an earlier
-- APPLIED or GRANTED was undone by a reversal or revocation. The undo
-- outcomes are a separate kind of answer: a REVERSED row never replaces the
-- APPLIED row it undoes, so "how did the change settle" and "was it undone"
-- are asked separately (outcome-filtered reads).
--
-- change_ref is the outbound change the provider is answering and
-- correlation_key the correlation the platform attached to it, so a receipt
-- can be tied back to its change without trusting anything but the signed
-- payload.
--
-- origin records how the receipt arrived: 'webhook' is a verified signed
-- callback; 'delivery_rejection' is the platform's own record of a provider
-- refusing the outbound request at intake (no callback will ever follow);
-- 'status_poll' is a state the platform read from the provider's status
-- endpoint (for example when a callback never arrived).
-- payload is the decoded callback body and payload_digest the sha256 of the
-- exact signed bytes; the digest, not the jsonb (which PostgreSQL normalizes),
-- is what decides whether a redelivery of the same event id is a duplicate
-- or a conflicting restatement. signal_id optionally links the receipt to the
-- signal it raised. secret_index records which of the endpoint's signing
-- secrets verified a webhook receipt (0 = current, higher = a previous
-- secret still honoured during rotation), for rotation telemetry; it is NULL
-- for the unsigned origins.
--
-- The table is append-only: a provider's statement is never edited, so what
-- was received is what stays on the record. SELECT and INSERT are granted;
-- UPDATE and DELETE are refused by the forbid_mutation trigger. One row per
-- (tenant, provider, event id) makes a redelivered callback converge.

-- +goose Up

CREATE TABLE integration_provider_receipt (
    tenant_id        tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    provider         text        NOT NULL,
    event_id         text        NOT NULL,
    event_type       text        NOT NULL,
    change_ref       text        NOT NULL,
    correlation_key  text        NOT NULL,
    outcome          text        NOT NULL,
    provider_ref     text        NOT NULL DEFAULT '',
    reason           text        NOT NULL DEFAULT '',
    origin           text        NOT NULL,
    payload          jsonb       NOT NULL,
    payload_digest   text        NOT NULL,
    signal_id        uuid,
    received_at      timestamptz NOT NULL,
    secret_index     smallint,
    PRIMARY KEY (tenant_id, provider, event_id),
    CONSTRAINT integration_provider_receipt_provider_known CHECK (provider IN ('payroll', 'iam')),
    CONSTRAINT integration_provider_receipt_outcome_known CHECK (outcome IN ('APPLIED', 'GRANTED', 'REJECTED', 'REVERSED', 'REVOKED')),
    CONSTRAINT integration_provider_receipt_origin_known CHECK (origin IN ('webhook', 'delivery_rejection', 'status_poll')),
    CONSTRAINT integration_provider_receipt_secret_index_webhook CHECK (secret_index IS NULL OR (origin = 'webhook' AND secret_index >= 0)),
    CONSTRAINT integration_provider_receipt_event_id_present CHECK (event_id <> '' AND char_length(event_id) <= 200),
    CONSTRAINT integration_provider_receipt_event_type_present CHECK (event_type <> ''),
    CONSTRAINT integration_provider_receipt_change_ref_present CHECK (change_ref <> '' AND char_length(change_ref) <= 200),
    CONSTRAINT integration_provider_receipt_correlation_present CHECK (correlation_key <> ''),
    CONSTRAINT integration_provider_receipt_digest_present CHECK (payload_digest <> ''),
    CONSTRAINT integration_provider_receipt_reason_bounded CHECK (char_length(reason) <= 2000)
);

CREATE INDEX integration_provider_receipt_by_change ON integration_provider_receipt (tenant_id, change_ref, received_at DESC);

ALTER TABLE integration_provider_receipt ENABLE ROW LEVEL SECURITY;
ALTER TABLE integration_provider_receipt FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON integration_provider_receipt
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON integration_provider_receipt TO hcmnext_app;

CREATE TRIGGER integration_provider_receipt_forbid_mutation
    BEFORE UPDATE OR DELETE ON integration_provider_receipt
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00313 is irreversible: migrations 00279-00302 already broke the rollback chain, so this migration keeps that true rather than claiming a reversibility no rollback can ever reach'; END $$;
-- +goose StatementEnd
