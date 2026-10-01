-- Preserve the accepted wake payload across worker restart before wait-step completion.
-- +goose Up
ALTER TABLE agent_task ADD COLUMN last_wake_event jsonb
    CHECK (last_wake_event IS NULL OR jsonb_typeof(last_wake_event) = 'object');

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM agent_task WHERE last_wake_event IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot remove retained accepted agent wake evidence';
    END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE agent_task DROP COLUMN last_wake_event;
