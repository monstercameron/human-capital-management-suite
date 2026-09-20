-- Mutable workflow-designer autosaves. Drafts are operational recovery state,
-- never publication history: only the governed publish path writes immutable
-- workflow_compiled_version rows.

-- +goose Up

CREATE TABLE workflow_designer_draft (
    tenant_id           tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    draft_id            uuid        NOT NULL,
    workflow_id         text        NOT NULL,
    author_ref          text        NOT NULL,
    base_version_digest text,
    revision            bigint      NOT NULL DEFAULT 1,
    document            jsonb       NOT NULL,
    expires_at          timestamptz NOT NULL,
    created_at          timestamptz NOT NULL,
    updated_at          timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, draft_id),
    CONSTRAINT workflow_designer_draft_workflow_present CHECK (length(btrim(workflow_id)) > 0),
    CONSTRAINT workflow_designer_draft_author_present CHECK (length(btrim(author_ref)) > 0),
    CONSTRAINT workflow_designer_draft_revision_positive CHECK (revision > 0),
    CONSTRAINT workflow_designer_draft_document_object CHECK (jsonb_typeof(document) = 'object'),
    CONSTRAINT workflow_designer_draft_expiry_after_creation CHECK (expires_at > created_at)
);

CREATE INDEX workflow_designer_draft_by_author
    ON workflow_designer_draft (tenant_id, author_ref, updated_at DESC, draft_id);
CREATE INDEX workflow_designer_draft_expiry
    ON workflow_designer_draft (expires_at, tenant_id, draft_id);

ALTER TABLE workflow_designer_draft ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_designer_draft FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workflow_designer_draft
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON workflow_designer_draft TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00314 is irreversible: migrations 00279-00313 already broke the rollback chain, so this migration keeps that true rather than claiming a reversibility no rollback can ever reach'; END $$;
-- +goose StatementEnd
