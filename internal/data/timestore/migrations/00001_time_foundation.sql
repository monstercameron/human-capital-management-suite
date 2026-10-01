-- +goose Up
-- Shared helpers for the time-keeping schema. Every later migration calls
-- time_enable_tenant_isolation for each tenant-scoped table it creates and
-- attaches time_forbid_mutation to every append-only table.

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION time_forbid_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'time append-only relation % cannot be changed', TG_TABLE_NAME; END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION time_enable_tenant_isolation(t text) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
 EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
 EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
 EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = current_setting(''hcmnext.tenant_id'', true)) WITH CHECK (tenant_id = current_setting(''hcmnext.tenant_id'', true))', t);
END $$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION time_enable_tenant_isolation(text);
DROP FUNCTION time_forbid_mutation();
