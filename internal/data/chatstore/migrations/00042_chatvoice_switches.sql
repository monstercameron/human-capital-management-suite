-- +goose Up
-- Voice switches (CHATVOICE-005): a workspace switch above everything, a channel
-- switch and a personal switch, all read on the server before any voice message
-- is accepted. No row means the decision record's default: voice on for the
-- workspace and for each person, off in a channel until its administrator
-- enables it.
CREATE TABLE chat_voice_setting (
    tenant_id text NOT NULL,
    scope text NOT NULL CHECK (scope IN ('workspace','channel','person')),
    scope_id text NOT NULL DEFAULT '',
    enabled boolean NOT NULL,
    updated_by text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, scope, scope_id),
    CHECK ((scope = 'workspace') = (scope_id = ''))
);
ALTER TABLE chat_voice_setting ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_voice_setting FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_voice_setting
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));

-- Every role that may update posts writes and, on a message's deletion, erases
-- the derived voice rows; a cell whose roles are separated gives the serving
-- role only SELECT, INSERT and UPDATE by default, so each of them is granted
-- here, DELETE included (the pattern of migrations 00028 and 00039). The
-- erasure trigger of migration 00026 runs as the caller.
-- +goose StatementBegin
DO $$ DECLARE role_name text; BEGIN
    FOR role_name IN SELECT DISTINCT grantee FROM information_schema.role_table_grants
      WHERE table_schema=current_schema() AND table_name='chat_post' AND privilege_type='UPDATE' AND grantee<>'PUBLIC'
    LOOP
      EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON chat_voice_setting TO %I',role_name);
      EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON chat_voice_transcript TO %I',role_name);
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE chat_voice_setting;
