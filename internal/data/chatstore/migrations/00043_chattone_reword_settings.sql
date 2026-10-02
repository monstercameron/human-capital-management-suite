-- +goose Up
-- CHATTONE-003: what an administrator chooses about "Reword heated messages"
-- and about whether members may read messages as written, for a workspace and,
-- as an override, for one channel; and what each person chooses to read. No
-- table here holds message text.

-- One row per workspace (channel_id is the empty string) and one per channel
-- override. mode is the administrator's "Reword heated messages": off, offered
-- to writers, or on. members_may_view_original is "Members may view messages
-- as written". A workspace with no row has rewording off and members may view
-- the original. Removing a channel's row returns it to the workspace's choice,
-- so the runtime role needs DELETE (granted below).
CREATE TABLE chattone_reword_setting (
    tenant_id text NOT NULL,
    channel_id text NOT NULL DEFAULT '',
    mode text NOT NULL CHECK (mode IN ('off','offered','on')),
    members_may_view_original boolean NOT NULL,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    updated_by text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, channel_id)
);

-- A person's choice for heated messages: as written or reworded, for every
-- conversation (channel_id is the empty string) and, as an override, for one.
-- The person's own row is only ever read and written for that person; the
-- service enforces it, row level security scopes it to the tenant. A person
-- who returns a channel to their general choice deletes the override row.
CREATE TABLE chattone_reader_choice (
    tenant_id text NOT NULL,
    person_id text NOT NULL,
    channel_id text NOT NULL DEFAULT '',
    tone text NOT NULL CHECK (tone IN ('as-written','reworded')),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, person_id, channel_id)
);

ALTER TABLE chattone_reword_setting ENABLE ROW LEVEL SECURITY;
ALTER TABLE chattone_reword_setting FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chattone_reword_setting USING (tenant_id=current_setting('hcmnext.tenant_id',true)) WITH CHECK (tenant_id=current_setting('hcmnext.tenant_id',true));
ALTER TABLE chattone_reader_choice ENABLE ROW LEVEL SECURITY;
ALTER TABLE chattone_reader_choice FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chattone_reader_choice USING (tenant_id=current_setting('hcmnext.tenant_id',true)) WITH CHECK (tenant_id=current_setting('hcmnext.tenant_id',true));

-- A setting is revised in place and its revision only moves forward.
-- +goose StatementBegin
CREATE FUNCTION chattone_reword_setting_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        NEW.revision := OLD.revision + 1;
        NEW.updated_at := now();
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER chattone_reword_setting_guard BEFORE UPDATE ON chattone_reword_setting FOR EACH ROW EXECUTE FUNCTION chattone_reword_setting_guard();

-- In a cell whose roles are separated the serving role holds only the schema's
-- default table privileges (SELECT, INSERT, UPDATE). Every role that may write
-- the workspace's writing-style setting may also remove a channel override and a
-- person's override row (row level security still scopes both to the tenant).
-- +goose StatementBegin
DO $$ DECLARE role_name text; BEGIN
    FOR role_name IN SELECT DISTINCT grantee FROM information_schema.role_table_grants
      WHERE table_schema=current_schema() AND table_name='chattone_workspace_setting' AND privilege_type='UPDATE' AND grantee<>'PUBLIC'
    LOOP
      EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON chattone_reword_setting TO %I',role_name);
      EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON chattone_reader_choice TO %I',role_name);
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE chattone_reader_choice;
DROP TABLE chattone_reword_setting;
DROP FUNCTION chattone_reword_setting_guard();
