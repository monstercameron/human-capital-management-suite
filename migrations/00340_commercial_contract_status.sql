-- Persist contract lifecycle state so frozen COMM-001 snapshots retain
-- suspension and revocation decisions across PostgreSQL round trips.
-- +goose Up
ALTER TABLE commercial_contract_revision
    ADD COLUMN IF NOT EXISTS status text NOT NULL DEFAULT 'ACTIVE'
    CHECK (status IN ('ACTIVE', 'SUSPENDED', 'REVOKED'));

-- +goose Down
ALTER TABLE commercial_contract_revision DROP COLUMN IF EXISTS status;
