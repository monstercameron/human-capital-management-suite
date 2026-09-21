-- Durable workflow-designer undo/redo history. The draft revision
-- remains monotonic for optimistic concurrency; history_position is a
-- separate cursor so undo and redo never reuse a revision number.

-- +goose Up

ALTER TABLE workflow_designer_draft
    ADD COLUMN history_position bigint NOT NULL DEFAULT 1,
    ADD COLUMN history_length   bigint NOT NULL DEFAULT 1,
    ADD CONSTRAINT workflow_designer_draft_history_position_positive CHECK (history_position > 0),
    ADD CONSTRAINT workflow_designer_draft_history_length_positive CHECK (history_length > 0),
    ADD CONSTRAINT workflow_designer_draft_history_cursor_bounded CHECK (history_position <= history_length);

CREATE TABLE workflow_designer_draft_history (
    tenant_id       tenant_ref  NOT NULL,
    draft_id        uuid        NOT NULL,
    history_position bigint     NOT NULL,
    command_label   text        NOT NULL,
    document        jsonb       NOT NULL,
    created_at      timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, draft_id, history_position),
    FOREIGN KEY (tenant_id, draft_id)
        REFERENCES workflow_designer_draft (tenant_id, draft_id)
        ON DELETE CASCADE,
    CONSTRAINT workflow_designer_draft_history_position_positive CHECK (history_position > 0),
    CONSTRAINT workflow_designer_draft_history_label_present CHECK (length(btrim(command_label)) > 0),
    CONSTRAINT workflow_designer_draft_history_document_object CHECK (jsonb_typeof(document) = 'object')
);

INSERT INTO workflow_designer_draft_history
    (tenant_id, draft_id, history_position, command_label, document, created_at)
SELECT tenant_id, draft_id, 1, 'Recovered draft', document, updated_at
FROM workflow_designer_draft;

CREATE INDEX workflow_designer_draft_history_by_draft
    ON workflow_designer_draft_history (tenant_id, draft_id, history_position);

ALTER TABLE workflow_designer_draft_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_designer_draft_history FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workflow_designer_draft_history
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, DELETE ON workflow_designer_draft_history TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00318 is irreversible: durable authoring history may already be referenced by an active editing session'; END $$;
-- +goose StatementEnd
