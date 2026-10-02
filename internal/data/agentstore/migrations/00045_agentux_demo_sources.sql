-- +goose Up
ALTER TABLE agent_run_request DROP CONSTRAINT agent_run_request_source_kind_check;
ALTER TABLE agent_run_request ADD CONSTRAINT agent_run_request_source_kind_check
    CHECK (source_kind IN ('CHAT','PERSONA_MENTION','ANNOUNCEMENT','API','EVENT','SCHEDULE','WORKFLOW','SUPPORT_EMAIL'));
ALTER TABLE agent_announcement DROP CONSTRAINT agent_announcement_document_references_check;
ALTER TABLE agent_announcement ADD CONSTRAINT agent_announcement_document_references_check
    CHECK (jsonb_typeof(document_references) = 'array' AND
        ((persona_id = 'hcmnext.local.persona.birthday_buddy' AND jsonb_array_length(document_references) = 0) OR
         (persona_id <> 'hcmnext.local.persona.birthday_buddy' AND jsonb_array_length(document_references) BETWEEN 1 AND 5)));

-- +goose Down
ALTER TABLE agent_announcement DROP CONSTRAINT agent_announcement_document_references_check;
ALTER TABLE agent_announcement ADD CONSTRAINT agent_announcement_document_references_check
    CHECK (jsonb_typeof(document_references) = 'array' AND jsonb_array_length(document_references) BETWEEN 1 AND 5);
ALTER TABLE agent_run_request DROP CONSTRAINT agent_run_request_source_kind_check;
ALTER TABLE agent_run_request ADD CONSTRAINT agent_run_request_source_kind_check
    CHECK (source_kind IN ('CHAT','PERSONA_MENTION','ANNOUNCEMENT','API','EVENT','SCHEDULE','WORKFLOW'));
