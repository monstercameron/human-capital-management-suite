-- Workflow release identities use canonical SemVer 2.0.0. Published versions
-- are immutable and unique inside a workflow family; mutable authoring drafts
-- carry the target release identity separately from their autosave revision.

-- +goose Up

ALTER TABLE workflow_designer_draft
    ADD COLUMN semantic_version text;

UPDATE workflow_designer_draft
SET semantic_version = '0.1.0'
WHERE semantic_version IS NULL;

ALTER TABLE workflow_designer_draft
    ALTER COLUMN semantic_version SET NOT NULL,
    ADD CONSTRAINT workflow_designer_draft_semver CHECK (
        semantic_version ~ '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-((0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*))?(\+([0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*))?$'
    );

ALTER TABLE workflow_compiled_version
    ADD CONSTRAINT workflow_compiled_version_semver CHECK (
        semantic_version ~ '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-((0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*))?(\+([0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*))?$'
    );

CREATE UNIQUE INDEX workflow_compiled_version_semantic_identity
    ON workflow_compiled_version (workflow_id, semantic_version);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00315 is irreversible: semantic workflow release identities may not be discarded after publication'; END $$;
-- +goose StatementEnd
