-- +goose Up
ALTER TABLE agent_run_request DROP CONSTRAINT agent_run_request_source_kind_check;
ALTER TABLE agent_run_request ADD CONSTRAINT agent_run_request_source_kind_check
    CHECK (source_kind IN ('CHAT','PERSONA_MENTION','ANNOUNCEMENT','API','EVENT','SCHEDULE','WORKFLOW'));

-- +goose Down
ALTER TABLE agent_run_request DROP CONSTRAINT agent_run_request_source_kind_check;
ALTER TABLE agent_run_request ADD CONSTRAINT agent_run_request_source_kind_check
    CHECK (source_kind IN ('CHAT','PERSONA_MENTION','API','EVENT','SCHEDULE','WORKFLOW'));
