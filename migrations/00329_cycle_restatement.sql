-- Owner: data plane. Phase: REV-039-01.
-- Durable, immutable BusinessCycle restatement records with one CAS winner per
-- tenant, cycle and prior close sequence.

-- +goose Up

CREATE TABLE cycle_restatement (
    row_id             uuid        NOT NULL,
    tenant_id          tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    restatement_id     text        NOT NULL,
    revision           cas_version NOT NULL,
    cycle_id           text        NOT NULL,
    prior_sequence     bigint      NOT NULL,
    prior_close_digest text        NOT NULL,
    digest             text        NOT NULL,
    correction_at      text        NOT NULL,
    canonical_body     jsonb       NOT NULL,
    recorded_at        timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT cycle_restatement_identity_unique UNIQUE (tenant_id, restatement_id),
    CONSTRAINT cycle_restatement_prior_cas_unique UNIQUE (tenant_id, cycle_id, prior_sequence),
    CONSTRAINT cycle_restatement_sequence_positive CHECK (prior_sequence > 0),
    CONSTRAINT cycle_restatement_digest_format CHECK (digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT cycle_restatement_prior_digest_format CHECK (prior_close_digest ~ '^sha256:[0-9a-f]{64}$')
);

CREATE OR REPLACE TRIGGER cycle_restatement_append_only
    BEFORE UPDATE OR DELETE ON cycle_restatement
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE cycle_restatement ENABLE ROW LEVEL SECURITY;
ALTER TABLE cycle_restatement FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON cycle_restatement
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
REVOKE UPDATE, DELETE ON cycle_restatement FROM PUBLIC;
REVOKE UPDATE, DELETE ON cycle_restatement FROM hcmnext_app;
GRANT SELECT, INSERT ON cycle_restatement TO hcmnext_app;

-- +goose Down
REVOKE ALL ON cycle_restatement FROM hcmnext_app;
DROP TABLE cycle_restatement;
