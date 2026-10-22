-- AGENTUX-023: name the authority role that inserts persona run policy
-- revisions. The serving role keeps SELECT (migration 00015); this role
-- adds INSERT so the preparation and production provisioning run as a
-- least-privilege session role instead of an owner connection.
-- +goose Up
-- +goose StatementBegin
DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hcmnext_agent_policy_authority') THEN
        CREATE ROLE hcmnext_agent_policy_authority NOLOGIN;
    END IF;
END $$;
-- +goose StatementEnd

ALTER ROLE hcmnext_agent_policy_authority
    NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOLOGIN NOREPLICATION NOBYPASSRLS;

-- Goose applies each store migration with the target schema pinned in
-- search_path. Grant usage on that schema without permitting object creation.
-- +goose StatementBegin
DO $$
DECLARE target_schema text := current_schema();
BEGIN
    EXECUTE format('GRANT USAGE ON SCHEMA %I TO hcmnext_agent_policy_authority', target_schema);
END
$$;
-- +goose StatementEnd

GRANT SELECT, INSERT ON persona_run_policy TO hcmnext_agent_policy_authority;

-- +goose Down

REVOKE INSERT, SELECT ON persona_run_policy FROM hcmnext_agent_policy_authority;
-- +goose StatementBegin
DO $$
DECLARE target_schema text := current_schema();
BEGIN
    EXECUTE format('REVOKE USAGE ON SCHEMA %I FROM hcmnext_agent_policy_authority', target_schema);
END
$$;
-- +goose StatementEnd
-- The cluster-wide hcmnext_agent_policy_authority role is retained because
-- deployment login roles may still be members of it; dropping it belongs to
-- credential lifecycle management, not schema rollback.
