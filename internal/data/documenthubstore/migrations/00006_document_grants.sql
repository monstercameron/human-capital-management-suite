-- HUB-011: separate action grants. Each row grants or denies one action to
-- one subject for one document. Grants are mutable policy records: expiry
-- passes, revocation marks, and revision counts changes, but rows are never
-- deleted so the policy history survives. Absence denies; an explicit deny
-- dominates any allow.
-- +goose Up
CREATE TABLE document_grant (
    id text PRIMARY KEY, tenant_id text NOT NULL, document_id text NOT NULL,
    subject_kind text NOT NULL DEFAULT 'person', subject_id text NOT NULL,
    action text NOT NULL, effect text NOT NULL DEFAULT 'allow',
    issuer_id text NOT NULL, purpose text NOT NULL DEFAULT '',
    expires_at timestamptz, revision bigint NOT NULL DEFAULT 1,
    revoked boolean NOT NULL DEFAULT false, revoked_at timestamptz, revoked_by text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, document_id) REFERENCES document(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT document_grant_action_check CHECK (action IN ('read','history','propose','review','deploy','manage','comment','export','retire')),
    CONSTRAINT document_grant_effect_check CHECK (effect IN ('allow','deny')),
    CONSTRAINT document_grant_subject_check CHECK (subject_kind IN ('person','team','channel','company'))
);
CREATE INDEX document_grant_lookup ON document_grant(tenant_id, document_id, subject_kind, subject_id, action);

-- +goose StatementBegin
DO $$ BEGIN
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', 'document_grant');
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', 'document_grant');
    EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = current_setting(''hcmnext.tenant_id'', true)) WITH CHECK (tenant_id = current_setting(''hcmnext.tenant_id'', true))', 'document_grant');
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE document_grant CASCADE;
