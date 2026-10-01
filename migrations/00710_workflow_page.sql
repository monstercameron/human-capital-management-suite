-- WFPAGE-011: tenant-owned workflow input page revisions and drafts.
-- The page version is an immutable content identity. A workflow instance keeps
-- a text reference so old runs remain explainable after a designer publishes a
-- newer page; legacy instances use the generated-default marker.

-- +goose Up

CREATE TABLE workflow_page_version (
    tenant_id          tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    workflow_key       semantic_key NOT NULL,
    workflow_version   integer      NOT NULL,
    page_id            semantic_key NOT NULL,
    page_version       integer      NOT NULL,
    definition         jsonb        NOT NULL,
    definition_digest  text         NOT NULL,
    generated_default  boolean      NOT NULL DEFAULT false,
    published_at       timestamptz  NOT NULL DEFAULT now(),
    published_by       text         NOT NULL,
    PRIMARY KEY (tenant_id, workflow_key, workflow_version, page_id, page_version),
    CONSTRAINT workflow_page_version_workflow_positive CHECK (workflow_version >= 1),
    CONSTRAINT workflow_page_version_page_positive CHECK (page_version >= 1),
    CONSTRAINT workflow_page_version_definition_object CHECK (jsonb_typeof(definition) = 'object'),
    CONSTRAINT workflow_page_version_digest_present CHECK (definition_digest <> ''),
    CONSTRAINT workflow_page_version_publisher_present CHECK (published_by <> '')
);

CREATE INDEX workflow_page_version_lookup
    ON workflow_page_version (tenant_id, workflow_key, workflow_version, page_id, page_version DESC);

CREATE TABLE workflow_page_draft (
    tenant_id          tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    draft_id           uuid         NOT NULL,
    workflow_key       semantic_key NOT NULL,
    workflow_version   integer      NOT NULL,
    page_id            semantic_key NOT NULL,
    draft_version      bigint       NOT NULL,
    definition         jsonb        NOT NULL,
    definition_digest  text         NOT NULL,
    author              text         NOT NULL,
    recorded_at         timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, draft_id, draft_version),
    CONSTRAINT workflow_page_draft_workflow_positive CHECK (workflow_version >= 1),
    CONSTRAINT workflow_page_draft_version_positive CHECK (draft_version >= 1),
    CONSTRAINT workflow_page_draft_definition_object CHECK (jsonb_typeof(definition) = 'object'),
    CONSTRAINT workflow_page_draft_digest_present CHECK (definition_digest <> ''),
    CONSTRAINT workflow_page_draft_author_present CHECK (author <> '')
);

CREATE INDEX workflow_page_draft_lookup
    ON workflow_page_draft (tenant_id, workflow_key, workflow_version, page_id, recorded_at DESC);

CREATE TRIGGER workflow_page_version_append_only
    BEFORE UPDATE OR DELETE ON workflow_page_version
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE TRIGGER workflow_page_draft_append_only
    BEFORE UPDATE OR DELETE ON workflow_page_draft
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE workflow_page_version ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_page_version FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workflow_page_version
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE workflow_page_draft ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_page_draft FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workflow_page_draft
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE workflow_instance ADD COLUMN page_version text NOT NULL DEFAULT 'generated-default';

GRANT SELECT, INSERT ON workflow_page_version, workflow_page_draft TO hcmnext_app;
GRANT SELECT, UPDATE ON workflow_instance TO hcmnext_app;

-- +goose Down

ALTER TABLE workflow_instance DROP COLUMN page_version;
DROP POLICY tenant_isolation ON workflow_page_draft;
DROP POLICY tenant_isolation ON workflow_page_version;
ALTER TABLE workflow_page_draft NO FORCE ROW LEVEL SECURITY;
ALTER TABLE workflow_page_draft DISABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_page_version NO FORCE ROW LEVEL SECURITY;
ALTER TABLE workflow_page_version DISABLE ROW LEVEL SECURITY;
DROP TABLE workflow_page_draft;
DROP TABLE workflow_page_version;
