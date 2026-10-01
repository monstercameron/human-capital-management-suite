-- +goose Up
ALTER TABLE agent_delegation_grant ADD COLUMN common_admission_id text;
ALTER TABLE agent_delegation_grant ADD CONSTRAINT agent_delegation_admission_shape CHECK (
    common_admission_id IS NULL OR (parent_grant_id IS NOT NULL AND length(common_admission_id) > 0)
);

-- +goose Down
ALTER TABLE agent_delegation_grant DROP CONSTRAINT agent_delegation_admission_shape;
ALTER TABLE agent_delegation_grant DROP COLUMN common_admission_id;
