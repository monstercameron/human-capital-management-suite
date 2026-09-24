-- REV-012-03: durable, deduplicated ledger invariant incident state and scan
-- evidence emitted by the running scheduler.

-- +goose Up

CREATE TABLE ledger_invariant_scan_state (
    tenant_id          tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    finding_key        text NOT NULL,
    generation         integer NOT NULL CHECK (generation > 0),
    incident_id        uuid NOT NULL,
    incident_key       semantic_key NOT NULL,
    status             text NOT NULL CHECK (status IN ('OPEN','RESOLVED')),
    first_seen_at      timestamptz NOT NULL,
    last_seen_at       timestamptz NOT NULL,
    evidence_digest    content_digest NOT NULL,
    finding_details    jsonb NOT NULL CHECK (jsonb_typeof(finding_details) = 'object'),
    PRIMARY KEY (tenant_id, finding_key),
    UNIQUE (tenant_id, incident_key),
    FOREIGN KEY (tenant_id, incident_id) REFERENCES operational_incident (tenant_id, incident_id),
    CHECK (length(finding_key) = 64),
    CHECK (last_seen_at >= first_seen_at)
);

CREATE TABLE ledger_invariant_scan_observation (
    tenant_id       tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    observation_id  uuid NOT NULL,
    quality         text NOT NULL CHECK (quality IN ('HEALTHY','DEGRADED','UNKNOWN')),
    watermark       bigint NOT NULL CHECK (watermark >= 0),
    evidence_digest content_digest NOT NULL,
    observed_at     timestamptz NOT NULL,
    finding_count   integer NOT NULL CHECK (finding_count >= 0),
    findings        jsonb NOT NULL CHECK (jsonb_typeof(findings) = 'array'),
    PRIMARY KEY (tenant_id, observation_id)
);

CREATE OR REPLACE TRIGGER ledger_invariant_scan_observation_append_only
    BEFORE UPDATE OR DELETE ON ledger_invariant_scan_observation
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE ledger_invariant_scan_state ENABLE ROW LEVEL SECURITY;
ALTER TABLE ledger_invariant_scan_state FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON ledger_invariant_scan_state
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE ledger_invariant_scan_observation ENABLE ROW LEVEL SECURITY;
ALTER TABLE ledger_invariant_scan_observation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON ledger_invariant_scan_observation
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

REVOKE UPDATE, DELETE ON ledger_invariant_scan_observation FROM PUBLIC;
REVOKE DELETE ON ledger_invariant_scan_state FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON ledger_invariant_scan_state TO hcmnext_app;
GRANT SELECT, INSERT ON ledger_invariant_scan_observation TO hcmnext_app;

-- +goose Down
REVOKE ALL ON ledger_invariant_scan_observation FROM hcmnext_app;
REVOKE ALL ON ledger_invariant_scan_state FROM hcmnext_app;
DROP TABLE ledger_invariant_scan_observation;
DROP TABLE ledger_invariant_scan_state;
