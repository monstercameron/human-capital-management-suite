-- Agent-owned database bootstrap. This migration is intentionally outside the
-- core migration tree: the local tenant table is a projection, not a cross-DB
-- foreign key to workflow/core tenancy.
-- +goose Up

SELECT pg_advisory_xact_lock(hashtext('migration:hcmnext_agent_app'));

-- +goose StatementBegin
DO $$
BEGIN
    CREATE ROLE hcmnext_agent_app
        NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
        NOLOGIN NOREPLICATION NOBYPASSRLS;
EXCEPTION
    WHEN duplicate_object OR unique_violation THEN NULL;
END
$$;
-- +goose StatementEnd

ALTER ROLE hcmnext_agent_app
    NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOLOGIN NOREPLICATION NOBYPASSRLS;

-- Goose applies each store migration with the target schema pinned in
-- search_path. Grant usage on that schema without permitting object creation.
-- +goose StatementBegin
DO $$
DECLARE target_schema text := current_schema();
BEGIN
    EXECUTE format('REVOKE CREATE ON SCHEMA %I FROM PUBLIC', target_schema);
    EXECUTE format('GRANT USAGE ON SCHEMA %I TO hcmnext_agent_app', target_schema);
END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION forbid_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'agent append-only relation % cannot be changed', TG_TABLE_NAME;
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION forbid_mutation() FROM PUBLIC;

CREATE TABLE tenant (
    tenant_id uuid PRIMARY KEY
);

ALTER TABLE tenant ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON tenant
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT ON tenant TO hcmnext_agent_app;

-- +goose Down

DROP TABLE tenant;
DROP FUNCTION forbid_mutation();
-- The cluster-wide hcmnext_agent_app role is retained because deployment
-- login roles may still be members of it; dropping it belongs to credential
-- lifecycle management, not schema rollback.
