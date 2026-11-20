-- +goose Up
-- Issue 3812: a conversation bound to a governed case. One row ties a
-- conversation to an opaque case reference (the case lives outside Chat), names
-- the case owner (the only person who may change who is in the conversation)
-- and says whether the conversation is restricted (it cannot be forwarded,
-- linked, or saved outside itself). A binding is never deleted: lifting it
-- stamps unbound_at and unbound_by, and bind and lift are both written to the
-- audit log in the same transaction. At most one binding is in force per
-- conversation.
CREATE TABLE chat_conversation_case (
    tenant_id text NOT NULL,
    id text NOT NULL CHECK (btrim(id) <> ''),
    conversation_id text NOT NULL CHECK (btrim(conversation_id) <> ''),
    case_ref text NOT NULL CHECK (btrim(case_ref) <> '' AND char_length(case_ref) <= 120),
    owner_id text NOT NULL CHECK (btrim(owner_id) <> ''),
    restricted boolean NOT NULL DEFAULT true,
    bound_by text NOT NULL CHECK (btrim(bound_by) <> ''),
    bound_at timestamptz NOT NULL DEFAULT now(),
    unbound_by text,
    unbound_at timestamptz,
    PRIMARY KEY (tenant_id, id),
    CHECK ((unbound_at IS NULL) = (unbound_by IS NULL))
);
CREATE UNIQUE INDEX chat_conversation_case_active ON chat_conversation_case (tenant_id, conversation_id) WHERE unbound_at IS NULL;
ALTER TABLE chat_conversation_case ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_conversation_case FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_conversation_case
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));

-- The store binds a conversation, reads the binding, and lifts it by update.
-- It never deletes one.
-- A serving role is given what the store runs against the table and nothing
-- more, whether its privileges came from the schema's defaults or from a grant
-- made before this table existed (migration 00041 has the history). Row level
-- security still scopes every statement to the caller's tenant.
-- +goose StatementBegin
DO $$ DECLARE role_name text; BEGIN
    FOR role_name IN SELECT DISTINCT grantee FROM information_schema.role_table_grants
      WHERE table_schema=current_schema() AND table_name='chat_post' AND privilege_type='UPDATE' AND grantee<>'PUBLIC'
    LOOP
      EXECUTE format('GRANT SELECT, INSERT, UPDATE ON chat_conversation_case TO %I',role_name);
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE chat_conversation_case;
