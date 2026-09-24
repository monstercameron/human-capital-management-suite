-- Owner: integration lane. Phase: PHASE_2.
-- INTG-018: persist authenticated provider callbacks and enqueue a durable
-- consumer notification in the same transaction.

-- +goose Up

CREATE TABLE integration_webhook_receipt (
    tenant_id       tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    receipt_id      uuid         NOT NULL,
    provider        text         NOT NULL,
    endpoint_id     text         NOT NULL,
    event_id        text         NOT NULL,
    event_type      text         NOT NULL,
    schema_ref      text         NOT NULL,
    payload_digest  text         NOT NULL,
    request_digest  text         NOT NULL,
    payload_bytes   bytea        NOT NULL,
    parsed_receipt  jsonb        NOT NULL,
    received_at     timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, receipt_id),
    CONSTRAINT integration_webhook_receipt_identity UNIQUE (tenant_id, provider, event_id),
    CONSTRAINT integration_webhook_receipt_provider CHECK (provider IN ('payroll', 'iam')),
    CONSTRAINT integration_webhook_receipt_endpoint_present CHECK (btrim(endpoint_id) <> ''),
    CONSTRAINT integration_webhook_receipt_event_present CHECK (event_id <> '' AND char_length(event_id) <= 200),
    CONSTRAINT integration_webhook_receipt_type_present CHECK (btrim(event_type) <> ''),
    CONSTRAINT integration_webhook_receipt_schema_present CHECK (btrim(schema_ref) <> ''),
    CONSTRAINT integration_webhook_receipt_payload_digest CHECK (payload_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT integration_webhook_receipt_request_digest CHECK (request_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT integration_webhook_receipt_payload_bounded CHECK (octet_length(payload_bytes) BETWEEN 1 AND 65536),
    CONSTRAINT integration_webhook_receipt_parsed_object CHECK (jsonb_typeof(parsed_receipt) = 'object')
);

CREATE INDEX integration_webhook_receipt_received
    ON integration_webhook_receipt (tenant_id, received_at DESC, receipt_id);

ALTER TABLE integration_webhook_receipt ENABLE ROW LEVEL SECURITY;
ALTER TABLE integration_webhook_receipt FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON integration_webhook_receipt
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON integration_webhook_receipt TO hcmnext_app;

CREATE TRIGGER integration_webhook_receipt_forbid_mutation
    BEFORE UPDATE OR DELETE ON integration_webhook_receipt
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

CREATE TABLE integration_webhook_outbox (
    tenant_id       tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    outbox_id       uuid         NOT NULL,
    receipt_id      uuid         NOT NULL,
    effect_identity semantic_key NOT NULL,
    ordering_key    semantic_key NOT NULL,
    schema_ref      semantic_key NOT NULL,
    payload         jsonb        NOT NULL,
    status          text         NOT NULL DEFAULT 'PENDING',
    attempts        integer      NOT NULL DEFAULT 0,
    available_at    timestamptz NOT NULL,
    created_at      timestamptz NOT NULL,
    updated_at      timestamptz NOT NULL,
    last_error      text,
    PRIMARY KEY (tenant_id, outbox_id),
    CONSTRAINT integration_webhook_outbox_receipt_fk
        FOREIGN KEY (tenant_id, receipt_id)
        REFERENCES integration_webhook_receipt (tenant_id, receipt_id),
    CONSTRAINT integration_webhook_outbox_receipt_unique UNIQUE (tenant_id, receipt_id),
    CONSTRAINT integration_webhook_outbox_effect_unique UNIQUE (tenant_id, effect_identity),
    CONSTRAINT integration_webhook_outbox_status CHECK (status IN ('PENDING', 'IN_FLIGHT', 'DELIVERED', 'FAILED', 'ABANDONED')),
    CONSTRAINT integration_webhook_outbox_attempts CHECK (attempts >= 0),
    CONSTRAINT integration_webhook_outbox_payload_object CHECK (jsonb_typeof(payload) = 'object')
);

CREATE INDEX integration_webhook_outbox_dispatch
    ON integration_webhook_outbox (tenant_id, status, available_at, ordering_key);

ALTER TABLE integration_webhook_outbox ENABLE ROW LEVEL SECURITY;
ALTER TABLE integration_webhook_outbox FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON integration_webhook_outbox
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON integration_webhook_outbox TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM integration_webhook_receipt) THEN
        RAISE EXCEPTION 'cannot remove durable provider webhook receipts';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE integration_webhook_outbox;
DROP TABLE integration_webhook_receipt;
