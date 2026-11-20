-- +goose Up
-- Issue 3783: information barriers. One row keeps two groups (teams of the
-- people directory) from sharing a conversation. The pair is stored in key
-- order (group_a < group_b, lower-cased) so the same pair is one rule whichever
-- side an administrator named first; the labels keep the spelling they typed.
-- A rule is never deleted: removing it stamps revoked_at and revoked_by, and
-- both changes are written to the audit log in the same transaction.
CREATE TABLE chat_information_barrier (
    tenant_id text NOT NULL,
    id text NOT NULL CHECK (btrim(id) <> ''),
    group_a text NOT NULL CHECK (btrim(group_a) <> '' AND char_length(group_a) <= 80),
    group_b text NOT NULL CHECK (btrim(group_b) <> '' AND char_length(group_b) <= 80),
    label_a text NOT NULL CHECK (btrim(label_a) <> '' AND char_length(label_a) <= 80),
    label_b text NOT NULL CHECK (btrim(label_b) <> '' AND char_length(label_b) <= 80),
    created_by text NOT NULL CHECK (btrim(created_by) <> ''),
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_by text,
    revoked_at timestamptz,
    PRIMARY KEY (tenant_id, id),
    CHECK (group_a < group_b),
    CHECK ((revoked_at IS NULL) = (revoked_by IS NULL))
);
CREATE UNIQUE INDEX chat_information_barrier_pair ON chat_information_barrier (tenant_id, group_a, group_b) WHERE revoked_at IS NULL;
ALTER TABLE chat_information_barrier ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_information_barrier FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_information_barrier
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));

-- The store adds a rule, reads the rules, and revokes one by update. It never
-- deletes one.
-- A serving role is given what the store runs against the table and nothing
-- more, whether its privileges came from the schema's defaults or from a grant
-- made before this table existed (migration 00041 has the history). Row level
-- security still scopes every statement to the caller's tenant.
-- +goose StatementBegin
DO $$ DECLARE role_name text; BEGIN
    FOR role_name IN SELECT DISTINCT grantee FROM information_schema.role_table_grants
      WHERE table_schema=current_schema() AND table_name='chat_post' AND privilege_type='UPDATE' AND grantee<>'PUBLIC'
    LOOP
      EXECUTE format('GRANT SELECT, INSERT, UPDATE ON chat_information_barrier TO %I',role_name);
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE chat_information_barrier;
