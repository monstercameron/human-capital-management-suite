-- AGENTUX-023: name the repair role for the chat-unrouted-repair operator
-- procedure. The role reads exactly the tables the repair predicate
-- inspects and executes exactly one function that removes the malformed
-- rows, so the preparation repairs as a least-privilege session role
-- instead of the chat database owner's connection.
--
-- The two DELETEs live in a SECURITY DEFINER function on purpose:
-- constraint checks on the twenty-six tables with a foreign key into
-- chat_conversation run as the deleter, so deleting through the role
-- would need SELECT on every one of them. The function keeps the role
-- narrow while the Go repair keeps the reviewed predicate.
-- +goose Up
-- +goose StatementBegin
DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hcmnext_chat_repair') THEN
        CREATE ROLE hcmnext_chat_repair NOLOGIN;
    END IF;
END $$;
-- +goose StatementEnd

ALTER ROLE hcmnext_chat_repair
    NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOLOGIN NOREPLICATION NOBYPASSRLS;

-- Goose applies each store migration with the target schema pinned in
-- search_path. Grant usage on that schema without permitting object creation.
-- +goose StatementBegin
DO $$
DECLARE target_schema text := current_schema();
BEGIN
    EXECUTE format('GRANT USAGE ON SCHEMA %I TO hcmnext_chat_repair', target_schema);
END
$$;
-- +goose StatementEnd

GRANT SELECT ON chat_conversation TO hcmnext_chat_repair;
-- Row locks, not row writes: the repair reads the candidate with SELECT FOR
-- UPDATE, which requires the UPDATE privilege. The role issues no UPDATE or
-- DELETE statements of its own; removal happens only in the function below.
GRANT UPDATE ON chat_conversation TO hcmnext_chat_repair;
GRANT SELECT ON chat_post TO hcmnext_chat_repair;
GRANT SELECT ON chat_membership TO hcmnext_chat_repair;
GRANT SELECT ON chat_public_audience_policy TO hcmnext_chat_repair;
GRANT SELECT ON chat_channel_policy TO hcmnext_chat_repair;
GRANT SELECT ON chat_share_grant TO hcmnext_chat_repair;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION chat_repair_delete_conversation(p_tenant_id text, p_conversation_id text)
RETURNS integer
LANGUAGE plpgsql
SECURITY DEFINER
AS $$
DECLARE
    removed integer;
BEGIN
    DELETE FROM chat_persona_channel_policy WHERE tenant_id = p_tenant_id AND conversation_id = p_conversation_id;
    DELETE FROM chat_conversation WHERE tenant_id = p_tenant_id AND id = p_conversation_id;
    GET DIAGNOSTICS removed = ROW_COUNT;
    RETURN removed;
END
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION chat_repair_delete_conversation(text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION chat_repair_delete_conversation(text, text) TO hcmnext_chat_repair;

-- +goose Down

REVOKE EXECUTE ON FUNCTION chat_repair_delete_conversation(text, text) FROM hcmnext_chat_repair;
DROP FUNCTION chat_repair_delete_conversation(text, text);
REVOKE SELECT ON chat_share_grant FROM hcmnext_chat_repair;
REVOKE SELECT ON chat_channel_policy FROM hcmnext_chat_repair;
REVOKE SELECT ON chat_public_audience_policy FROM hcmnext_chat_repair;
REVOKE SELECT ON chat_membership FROM hcmnext_chat_repair;
REVOKE SELECT ON chat_post FROM hcmnext_chat_repair;
REVOKE UPDATE ON chat_conversation FROM hcmnext_chat_repair;
REVOKE SELECT ON chat_conversation FROM hcmnext_chat_repair;
-- +goose StatementBegin
DO $$
DECLARE target_schema text := current_schema();
BEGIN
    EXECUTE format('REVOKE USAGE ON SCHEMA %I FROM hcmnext_chat_repair', target_schema);
END
$$;
-- +goose StatementEnd
-- The cluster-wide hcmnext_chat_repair role is retained because deployment
-- login roles may still be members of it; dropping it belongs to credential
-- lifecycle management, not schema rollback.
