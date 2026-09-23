-- Owner: trust and product experience (RBAC-RT-021). Phase: production frontend.
-- storage-disposition: role authorization | append-only permission revision ledger | local PostgreSQL | tenant-local ACID/CAS | admin governed.
--
-- access_role_revision is the append-only revision ledger behind every role
-- authorization write. Each row records one role's before and after image
-- for a single change, so a change that writes rows for more than one role
-- saves one revision per role. The writer inserts the revision in the same
-- transaction as the permission change; the forbid_mutation trigger keeps
-- the ledger SELECT/INSERT-only afterwards.

-- +goose Up

CREATE TABLE IF NOT EXISTS access_role_revision (
    tenant_id      tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    revision_id    uuid NOT NULL,
    recorded_at    timestamptz NOT NULL DEFAULT clock_timestamp(),
    actor_ref      text NOT NULL CHECK (actor_ref <> ''),
    change_kind    text NOT NULL CHECK (change_kind IN ('ROLE','ASSIGNMENT','VISIBILITY','PAGE_PERMISSION','FEATURE_PERMISSION')),
    role_id        text NOT NULL CHECK (role_id ~ '^[a-z][a-z0-9_]{1,62}$'),
    worker_ref     text NOT NULL DEFAULT '',
    page_id        text NOT NULL DEFAULT '',
    feature_id     text NOT NULL DEFAULT '',
    before_row     jsonb,
    after_row      jsonb,
    prior_revision uuid,
    PRIMARY KEY (tenant_id, revision_id)
);

ALTER TABLE access_role_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE access_role_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON access_role_revision USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

CREATE TRIGGER access_role_revision_append_only
    BEFORE UPDATE OR DELETE ON access_role_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

GRANT SELECT, INSERT ON access_role_revision TO hcmnext_app;

-- +goose Down

-- Revision rows are durable authorization history and may already be cited
-- by later decisions. Dropping the ledger would erase that evidence.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00327 is irreversible: role authorization revisions are append-only evidence and cannot be discarded'; END $$;
-- +goose StatementEnd
