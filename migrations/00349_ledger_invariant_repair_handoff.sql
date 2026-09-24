-- REV-012-03: fence delayed scan receipts and persist an operator repair handoff.

-- +goose Up

CREATE TABLE ledger_invariant_scan_cursor (
    tenant_id        tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    last_observed_at timestamptz NOT NULL,
    evidence_digest  content_digest NOT NULL,
    PRIMARY KEY (tenant_id)
);

CREATE TABLE ledger_invariant_repair_work (
    tenant_id       tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    repair_id       uuid NOT NULL,
    finding_key     text NOT NULL CHECK (length(finding_key) = 64),
    generation      integer NOT NULL CHECK (generation > 0),
    incident_id     uuid NOT NULL,
    incident_key    semantic_key NOT NULL,
    status          text NOT NULL CHECK (status IN ('OPEN','RESOLVED')),
    opened_at       timestamptz NOT NULL,
    updated_at      timestamptz NOT NULL CHECK (updated_at >= opened_at),
    evidence_digest content_digest NOT NULL,
    finding_details jsonb NOT NULL CHECK (jsonb_typeof(finding_details) = 'object'),
    PRIMARY KEY (tenant_id, repair_id),
    UNIQUE (tenant_id, finding_key, generation),
    FOREIGN KEY (tenant_id, incident_id)
        REFERENCES operational_incident (tenant_id, incident_id)
);

CREATE INDEX ledger_invariant_repair_work_open
    ON ledger_invariant_repair_work (tenant_id, updated_at)
    WHERE status = 'OPEN';

ALTER TABLE ledger_invariant_scan_cursor ENABLE ROW LEVEL SECURITY;
ALTER TABLE ledger_invariant_scan_cursor FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON ledger_invariant_scan_cursor
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE ledger_invariant_repair_work ENABLE ROW LEVEL SECURITY;
ALTER TABLE ledger_invariant_repair_work FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON ledger_invariant_repair_work
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

REVOKE DELETE ON ledger_invariant_scan_cursor FROM PUBLIC;
REVOKE DELETE ON ledger_invariant_repair_work FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON ledger_invariant_scan_cursor TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON ledger_invariant_repair_work TO hcmnext_app;

-- +goose Down
REVOKE ALL ON ledger_invariant_repair_work FROM hcmnext_app;
REVOKE ALL ON ledger_invariant_scan_cursor FROM hcmnext_app;
DROP TABLE ledger_invariant_repair_work;
DROP TABLE ledger_invariant_scan_cursor;
