-- Owner: control-plane durability (REV-031-01 / REV-043-03).
-- storage-disposition: applied CP-008 kill switches and signed receipts | tenant-scoped durable control state | local PostgreSQL | tenant-local ACID | operator incident policy.
--
-- +goose Up

CREATE TABLE configbundle_kill_switch (
    tenant_id uuid NOT NULL REFERENCES tenant (tenant_id),
    switch_id text NOT NULL CHECK (switch_id <> ''),
    switch_digest text NOT NULL CHECK (switch_digest ~ '^sha256:[0-9a-f]{64}$'),
    receipt_digest text NOT NULL CHECK (receipt_digest ~ '^sha256:[0-9a-f]{64}$'),
    applied_record jsonb NOT NULL CHECK (jsonb_typeof(applied_record) = 'object'),
    PRIMARY KEY (tenant_id, switch_id),
    CHECK (applied_record->'switch'->>'Digest' = switch_digest),
    CHECK (applied_record->'receipt'->>'Digest' = receipt_digest),
    CHECK (applied_record->'switch'->'Request'->'Target'->>'TenantID' = tenant_id::text)
);

ALTER TABLE configbundle_kill_switch ENABLE ROW LEVEL SECURITY;
ALTER TABLE configbundle_kill_switch FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON configbundle_kill_switch
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE TRIGGER configbundle_kill_switch_append_only BEFORE UPDATE OR DELETE ON configbundle_kill_switch FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
GRANT SELECT, INSERT ON configbundle_kill_switch TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00339 is irreversible: applied emergency-switch evidence is append-only'; END $$;
-- +goose StatementEnd
