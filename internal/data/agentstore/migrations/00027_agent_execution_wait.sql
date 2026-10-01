-- Execution wait and retry state must survive worker process restart.
-- +goose Up
ALTER TABLE agent_run_execution
    ADD COLUMN retryable boolean NOT NULL DEFAULT false,
    ADD COLUMN wait_kind text NOT NULL DEFAULT '' CHECK (wait_kind IN ('','SIGNAL','TIMER','APPROVAL','USER_INPUT','WORKFLOW')),
    ADD COLUMN wait_ref text NOT NULL DEFAULT '';
ALTER TABLE agent_run_execution ADD CONSTRAINT agent_execution_wait_pair CHECK ((wait_kind='') = (wait_ref=''));

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM agent_run_execution WHERE retryable OR wait_kind<>'') THEN
        RAISE EXCEPTION 'cannot remove retained agent execution wait and retry state';
    END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE agent_run_execution DROP CONSTRAINT agent_execution_wait_pair;
ALTER TABLE agent_run_execution DROP COLUMN wait_ref, DROP COLUMN wait_kind, DROP COLUMN retryable;
