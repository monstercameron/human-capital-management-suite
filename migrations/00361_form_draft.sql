-- REV-025-01: persist encrypted, revisioned human form drafts.
--
-- +goose Up

CREATE TABLE form_draft (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    draft_id text NOT NULL CHECK (btrim(draft_id) <> ''),
    principal_id text NOT NULL CHECK (btrim(principal_id) <> ''),
    form_id text NOT NULL CHECK (btrim(form_id) <> ''),
    form_version text NOT NULL CHECK (btrim(form_version) <> ''),
    revision bigint NOT NULL CHECK (revision > 0),
    expires_at timestamptz NOT NULL,
    answer_digest bytea NOT NULL CHECK (octet_length(answer_digest) = 32),
    sealed_answers bytea NOT NULL CHECK (octet_length(sealed_answers) >= 29),
    PRIMARY KEY (tenant_id, draft_id)
);

CREATE INDEX form_draft_expiry ON form_draft (tenant_id, expires_at);

ALTER TABLE form_draft ENABLE ROW LEVEL SECURITY;
ALTER TABLE form_draft FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON form_draft
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE, DELETE ON form_draft TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM form_draft) THEN
        RAISE EXCEPTION 'cannot remove live encrypted form drafts';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE form_draft;
