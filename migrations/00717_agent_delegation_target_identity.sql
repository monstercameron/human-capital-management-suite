-- AGENTP-008: preserve the exact immutable target agent identity separately
-- from its version label. Empty remains the legacy/unbound value.

-- +goose Up
ALTER TABLE agent_delegation_grant
    ADD COLUMN target_agent_id text NOT NULL DEFAULT ''
    CHECK (target_agent_id = '' OR (btrim(target_agent_id) <> '' AND target_agent_id = btrim(target_agent_id)));

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM agent_delegation_grant WHERE target_agent_id <> '') THEN
        RAISE EXCEPTION 'cannot remove retained delegation target identities';
    END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE agent_delegation_grant DROP COLUMN target_agent_id;
