-- HUB-006: candidate status on versions. New submits enter as candidates;
-- review (HUB-008) and deployment (HUB-009) advance the status through the
-- lifecycle the hub spec defines. The check keeps the column inside that
-- vocabulary; the version rows themselves stay append-only.
-- +goose Up
ALTER TABLE document_version ADD COLUMN status text NOT NULL DEFAULT 'candidate';
ALTER TABLE document_version ADD CONSTRAINT document_version_status_check
    CHECK (status IN ('candidate','submitted','reviewed','deployed','stale','retired'));

-- +goose Down
ALTER TABLE document_version DROP CONSTRAINT document_version_status_check;
ALTER TABLE document_version DROP COLUMN status;
