-- HUB-016: policy epoch on documents. Every grant mutation bumps the
-- epoch and publishes a grant.revised event in the same commit, so derived
-- consumers (search indexes, caches) can invalidate without missing a
-- revoke. The genesis epoch is silent: creation bootstrap grants are the
-- starting policy, not a change.
-- +goose Up
ALTER TABLE document ADD COLUMN policy_epoch bigint NOT NULL DEFAULT 1;

-- +goose Down
ALTER TABLE document DROP COLUMN policy_epoch;
