-- UXBLIND-122: the tenant-level agents setting. Agents are off for a tenant
-- with no row; an administrator turns them on from Chat settings. Each change
-- advances the revision and records the acting subject.

-- +goose Up

CREATE TABLE tenant_agent_setting (
    tenant_id  uuid        NOT NULL PRIMARY KEY REFERENCES tenant(tenant_id),
    enabled    boolean     NOT NULL,
    revision   bigint      NOT NULL CHECK (revision >= 1),
    updated_by text        NOT NULL CHECK (btrim(updated_by) <> ''),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- +goose StatementBegin
CREATE FUNCTION tenant_agent_setting_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'table % keeps the tenant agents setting; DELETE is forbidden', TG_TABLE_NAME
            USING ERRCODE = '23514';
    END IF;
    IF NEW.tenant_id <> OLD.tenant_id OR NEW.revision <> OLD.revision + 1 THEN
        RAISE EXCEPTION 'the tenant is fixed and each change advances the revision by one'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER tenant_agent_setting_guard
    BEFORE UPDATE OR DELETE ON tenant_agent_setting
    FOR EACH ROW EXECUTE FUNCTION tenant_agent_setting_guard();

ALTER TABLE tenant_agent_setting ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_agent_setting FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON tenant_agent_setting
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE ON tenant_agent_setting TO hcmnext_app;

-- +goose Down
DROP TABLE tenant_agent_setting;
DROP FUNCTION tenant_agent_setting_guard();
