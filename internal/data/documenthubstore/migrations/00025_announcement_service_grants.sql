-- +goose Up
ALTER TABLE document_grant DROP CONSTRAINT document_grant_subject_check;
ALTER TABLE document_grant ADD CONSTRAINT document_grant_subject_check
    CHECK (subject_kind IN ('person','team','channel','company','service'));

-- +goose Down
ALTER TABLE document_grant DROP CONSTRAINT document_grant_subject_check;
ALTER TABLE document_grant ADD CONSTRAINT document_grant_subject_check
    CHECK (subject_kind IN ('person','team','channel','company'));
