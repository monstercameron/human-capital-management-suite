-- +goose Up
-- WTIME-011/012 and TCLOCK-015 persistence: destination records for
-- contractor invoice drafts, agency/VMS export records and payroll export
-- records, with the receiver's acceptance or rejection observed as an
-- append-only receipt trail against a content digest of the sent payload.

CREATE TABLE destination_record (
 tenant_id text NOT NULL, id text NOT NULL,
 kind text NOT NULL CHECK (kind IN ('CONTRACTOR_INVOICE','AGENCY_EXPORT','PAYROLL_EXPORT')),
 source_ref text NOT NULL, source_revision bigint NOT NULL CHECK (source_revision > 0),
 receiver_ref text NOT NULL,
 status text NOT NULL DEFAULT 'DRAFT' CHECK (status IN ('DRAFT','SENT','ACCEPTED','REJECTED')),
 revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
 payload_digest text NOT NULL CHECK (payload_digest <> ''), payload jsonb NOT NULL,
 idempotency_key text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id, id),
 UNIQUE (tenant_id, kind, source_ref, source_revision, idempotency_key)
);
CREATE INDEX destination_record_by_source ON destination_record(tenant_id, kind, source_ref);

CREATE TABLE destination_receipt (
 tenant_id text NOT NULL, id text NOT NULL, destination_id text NOT NULL,
 status text NOT NULL CHECK (status IN ('SENT','ACCEPTED','REJECTED')),
 receiver_ref text NOT NULL, reason text NOT NULL DEFAULT '',
 received_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id, id),
 FOREIGN KEY (tenant_id, destination_id) REFERENCES destination_record(tenant_id, id)
);
CREATE INDEX destination_receipt_history ON destination_receipt(tenant_id, destination_id, received_at);
CREATE TRIGGER destination_receipt_immutable BEFORE UPDATE OR DELETE ON destination_receipt
 FOR EACH ROW EXECUTE FUNCTION time_forbid_mutation();

SELECT time_enable_tenant_isolation('destination_record');
SELECT time_enable_tenant_isolation('destination_receipt');

-- +goose Down
DROP TABLE destination_receipt, destination_record CASCADE;
