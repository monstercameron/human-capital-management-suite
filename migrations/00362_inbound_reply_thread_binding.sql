-- Owner: communications lane. Phase: PHASE_2.
-- MSG-011: bind outbound replies to their canonical conversation thread.

-- +goose Up

ALTER TABLE recipient_message
    ADD COLUMN conversation_thread_id uuid;

ALTER TABLE recipient_message
    ADD CONSTRAINT recipient_message_conversation_thread_fk
    FOREIGN KEY (tenant_id, conversation_thread_id)
    REFERENCES conversation_thread (tenant_id, thread_id);

CREATE INDEX recipient_message_conversation_thread
    ON recipient_message (tenant_id, conversation_thread_id)
    WHERE conversation_thread_id IS NOT NULL;

-- +goose Down

DROP INDEX recipient_message_conversation_thread;
ALTER TABLE recipient_message DROP CONSTRAINT recipient_message_conversation_thread_fk;
ALTER TABLE recipient_message DROP COLUMN conversation_thread_id;
