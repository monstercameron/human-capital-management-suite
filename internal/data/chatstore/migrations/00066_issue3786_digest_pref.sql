-- +goose Up
-- Issue 3786: the personal digest. A person chooses whether they get a digest
-- of what was held back from them (mentions that arrived in quiet hours or in a
-- muted conversation, and followed threads that moved) and when. This table
-- holds only that choice and the moment the last digest was seen. It holds no
-- message text and no message ids: the digest itself is worked out when it is
-- read, from messages the person can read right then.
CREATE TABLE chat_digest_pref (
    tenant_id text NOT NULL,
    member_id text NOT NULL,
    cadence text NOT NULL DEFAULT 'off' CHECK (cadence IN ('off','daily','quiet_end')),
    minute integer NOT NULL DEFAULT 480 CHECK (minute BETWEEN 0 AND 1439),
    timezone text NOT NULL DEFAULT 'UTC',
    last_digest_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, member_id)
);
ALTER TABLE chat_digest_pref ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_digest_pref FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_digest_pref
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));

-- The store writes a person's choice, replaces it, and reads it. It never
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
      EXECUTE format('GRANT SELECT, INSERT, UPDATE ON chat_digest_pref TO %I',role_name);
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE chat_digest_pref;
