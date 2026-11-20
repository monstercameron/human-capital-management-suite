-- +goose Up
-- Issue 3920: a person's weekly quiet-hours schedule and one-off pause. It sits
-- beside chat_quiet_hours, which keeps the daily interval and the time zone
-- unchanged, so a person who never opens the weekly schedule sees no change in
-- delivery. A missing row means "no weekly schedule, no pause".
CREATE TABLE chat_quiet_schedule (
    tenant_id text NOT NULL,
    member_id text NOT NULL,
    week_active boolean NOT NULL DEFAULT false,
    days jsonb NOT NULL CHECK (jsonb_typeof(days) = 'array' AND jsonb_array_length(days) = 7),
    pause_kind text NOT NULL DEFAULT '' CHECK (pause_kind IN ('','pause','notify')),
    pause_until timestamptz,
    revision bigint NOT NULL DEFAULT 2 CHECK (revision >= 2),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, member_id),
    CHECK ((pause_kind = '') = (pause_until IS NULL))
);
ALTER TABLE chat_quiet_schedule ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_quiet_schedule FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_quiet_schedule
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));

-- The store writes a person's schedule, replaces it, and reads it. It never
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
      EXECUTE format('GRANT SELECT, INSERT, UPDATE ON chat_quiet_schedule TO %I',role_name);
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE chat_quiet_schedule;
