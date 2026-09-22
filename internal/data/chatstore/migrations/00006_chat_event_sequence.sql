-- +goose Up
ALTER TABLE chat_conversation ADD COLUMN event_sequence bigint NOT NULL DEFAULT 0;
ALTER TABLE chat_outbox DROP CONSTRAINT chat_outbox_tenant_id_aggregate_id_event_type_created_at_key;

-- +goose Down
ALTER TABLE chat_conversation DROP COLUMN event_sequence;
ALTER TABLE chat_outbox ADD CONSTRAINT chat_outbox_tenant_id_aggregate_id_event_type_created_at_key UNIQUE (tenant_id, aggregate_id, event_type, created_at);
