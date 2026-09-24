-- Owner: knowledge domain (REV-077-02). Phase: employee help search.
-- storage-disposition: article role scope and retention cutoff | immutable article revisions | tenant local | governed schedules | search only after activation.
--
-- +goose Up

ALTER TABLE knowledge_article_revision
    ADD COLUMN authorized_roles jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN retention_schedule_ref text NOT NULL DEFAULT '',
    ADD COLUMN retain_until timestamptz;

ALTER TABLE knowledge_article_revision
    ADD CONSTRAINT knowledge_article_revision_authorized_roles_array
        CHECK (jsonb_typeof(authorized_roles) = 'array'),
    ADD CONSTRAINT knowledge_article_revision_retention_ref_nonblank
        CHECK (retention_schedule_ref = '' OR btrim(retention_schedule_ref) <> '');

CREATE INDEX knowledge_article_search_activation_idx
    ON knowledge_article_revision (tenant_id, locale, article_id, revision)
    WHERE retention_schedule_ref <> '' AND jsonb_array_length(authorized_roles) > 0;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00333 is irreversible: authorized search scope and retention declarations are attached to immutable article revisions'; END $$;
-- +goose StatementEnd
