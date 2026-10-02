-- +goose Up
ALTER TABLE document_grant ADD COLUMN announcement_scope_id text NOT NULL DEFAULT '';
ALTER TABLE document_grant ADD CONSTRAINT announcement_scope_service_read
    CHECK (announcement_scope_id = '' OR (subject_kind = 'service' AND action = 'read' AND purpose LIKE 'announcement:%'));

-- +goose Down
ALTER TABLE document_grant DROP CONSTRAINT announcement_scope_service_read;
ALTER TABLE document_grant DROP COLUMN announcement_scope_id;
