-- HUB-015: bilateral cross-company document grants. A host tenant proposes
-- one document's cross-company scope with classification, residency and a
-- mandatory expiry; a consumer tenant administrator must separately accept
-- before the grant is current, mirroring
-- internal/collaboration/chatpolicy's ConversationGrantTerms for chat. This
-- table carries only the bilateral terms and consent: every read or export
-- across the boundary also requires an explicit document_grant row
-- (subject_kind='company' or 'person'), so the bilateral terms alone can
-- never open a wider door than an in-tenant share would. Rows are mutable
-- policy records like document_grant: accept flips one flag, revoke closes
-- one, and no row is ever deleted.
-- +goose Up
CREATE TABLE document_crosscompany_grant (
    id text PRIMARY KEY, tenant_id text NOT NULL, document_id text NOT NULL,
    host_tenant text NOT NULL, consumer_tenant text NOT NULL,
    classification text NOT NULL, residency text NOT NULL,
    version bigint NOT NULL DEFAULT 1,
    proposed boolean NOT NULL DEFAULT false, accepted_by_host boolean NOT NULL DEFAULT false,
    accepted_by_consumer boolean NOT NULL DEFAULT false,
    expires_at timestamptz NOT NULL, revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, document_id) REFERENCES document(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT document_crosscompany_grant_distinct CHECK (host_tenant <> consumer_tenant)
);
CREATE INDEX document_crosscompany_grant_lookup ON document_crosscompany_grant(tenant_id, document_id, consumer_tenant, created_at DESC);

-- +goose StatementBegin
DO $$ BEGIN
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', 'document_crosscompany_grant');
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', 'document_crosscompany_grant');
    EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = current_setting(''hcmnext.tenant_id'', true)) WITH CHECK (tenant_id = current_setting(''hcmnext.tenant_id'', true))', 'document_crosscompany_grant');
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE document_crosscompany_grant CASCADE;
