-- NAAS-003: recipient keyset feeds. Creation time does not move on read/pin.
-- These non-concurrent builds require a maintenance window on populated cells;
-- rehearse the migration on a production-sized copy before deployment.
-- +goose Up
CREATE INDEX inbox_record_feed ON inbox_record
    (tenant_id, subject_ref, archived, created_at DESC, inbox_record_id DESC);
CREATE INDEX inbox_record_read_feed ON inbox_record
    (tenant_id, subject_ref, archived, read_state, created_at DESC, inbox_record_id DESC);
CREATE INDEX inbox_record_pinned_feed ON inbox_record
    (tenant_id, subject_ref, archived, created_at DESC, inbox_record_id DESC) WHERE pinned;
CREATE INDEX message_intent_workflow_notice ON message_intent
    (tenant_id, workflow_ref, message_intent_id) WHERE template_key = 'workflow.attention';

-- +goose Down
DROP INDEX message_intent_workflow_notice;
DROP INDEX inbox_record_pinned_feed;
DROP INDEX inbox_record_read_feed;
DROP INDEX inbox_record_feed;
