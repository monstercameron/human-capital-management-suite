-- +goose Up
-- Issue 3789: a person's own away setting: the date they are away until, who
-- they say covers, whether colleagues may see it, and whether the cover is told
-- about mentions. It is a chat preference only. It grants the cover no access to
-- any conversation and no authority over any approval. A missing row means "not
-- away".
CREATE TABLE chat_away (
    tenant_id text NOT NULL,
    member_id text NOT NULL,
    away_since timestamptz,
    away_until timestamptz,
    cover_member_id text NOT NULL DEFAULT '',
    hidden boolean NOT NULL DEFAULT false,
    notify_cover boolean NOT NULL DEFAULT false,
    revision bigint NOT NULL DEFAULT 2 CHECK (revision >= 2),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, member_id),
    CHECK ((away_until IS NULL) = (away_since IS NULL)),
    CHECK (away_until IS NOT NULL OR (cover_member_id = '' AND NOT notify_cover)),
    CHECK (cover_member_id <> member_id),
    CHECK (NOT notify_cover OR cover_member_id <> '')
);
CREATE INDEX chat_away_cover_idx ON chat_away (tenant_id, cover_member_id) WHERE cover_member_id <> '';
ALTER TABLE chat_away ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_away FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_away
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));

-- The store writes a person's away setting, replaces it, and reads it. It
-- never deletes one.
-- A serving role is given what the store runs against the table and nothing
-- more, whether its privileges came from the schema's defaults or from a grant
-- made before this table existed (migration 00041 has the history). Row level
-- security still scopes every statement to the caller's tenant.
-- +goose StatementBegin
DO $$ DECLARE role_name text; BEGIN
    FOR role_name IN SELECT DISTINCT grantee FROM information_schema.role_table_grants
      WHERE table_schema=current_schema() AND table_name='chat_post' AND privilege_type='UPDATE' AND grantee<>'PUBLIC'
    LOOP
      EXECUTE format('GRANT SELECT, INSERT, UPDATE ON chat_away TO %I',role_name);
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE chat_away;
