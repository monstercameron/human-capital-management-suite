-- Persisted owner inputs for worker onboarding; current snapshot, never a ready seed.
-- +goose Up
CREATE TABLE worker_onboarding_snapshot (
 tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
 worker_id text NOT NULL,
 snapshot_revision bigint NOT NULL CHECK (snapshot_revision > 0),
 snapshot_payload jsonb NOT NULL CHECK (jsonb_typeof(snapshot_payload) = 'object'),
 payload_digest text NOT NULL CHECK (btrim(payload_digest) <> ''),
 PRIMARY KEY (tenant_id, worker_id)
);
ALTER TABLE worker_onboarding_snapshot ENABLE ROW LEVEL SECURITY;
ALTER TABLE worker_onboarding_snapshot FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON worker_onboarding_snapshot
 USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
 WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE, DELETE ON worker_onboarding_snapshot TO hcmnext_app;
-- +goose Down
DROP TABLE worker_onboarding_snapshot;
