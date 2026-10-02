-- +goose Up
CREATE TABLE chat_filter_version (
    tenant_id text NOT NULL,
    rule_id text NOT NULL,
    version text NOT NULL CHECK (version ~ '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'),
    definition jsonb NOT NULL,
    PRIMARY KEY (tenant_id, rule_id, version)
);
CREATE TABLE chat_filter_enablement (
    tenant_id text NOT NULL,
    rule_id text NOT NULL,
    channel_id text NOT NULL DEFAULT '',
    enabled boolean NOT NULL,
    action text NOT NULL DEFAULT '',
    dry_run_until timestamptz,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    PRIMARY KEY (tenant_id, rule_id, channel_id)
);
CREATE TABLE chat_filter_hit (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tenant_id text NOT NULL,
    channel_id text NOT NULL,
    subject_id text NOT NULL,
    created_at timestamptz NOT NULL,
    rule_id text NOT NULL,
    rule_version text NOT NULL,
    hit jsonb NOT NULL,
    CHECK (hit->>'Digest' ~ '^sha256:[a-f0-9]{64}$'),
    CHECK (hit->>'Masked' = '[removed word]')
);
CREATE INDEX chat_filter_hit_tenant ON chat_filter_hit (tenant_id, created_at, id);
ALTER TABLE chat_filter_version ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_filter_version FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_filter_version USING (tenant_id=current_setting('hcmnext.tenant_id',true)) WITH CHECK (tenant_id=current_setting('hcmnext.tenant_id',true));
ALTER TABLE chat_filter_enablement ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_filter_enablement FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_filter_enablement USING (tenant_id=current_setting('hcmnext.tenant_id',true)) WITH CHECK (tenant_id=current_setting('hcmnext.tenant_id',true));
ALTER TABLE chat_filter_hit ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_filter_hit FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_filter_hit USING (tenant_id=current_setting('hcmnext.tenant_id',true)) WITH CHECK (tenant_id=current_setting('hcmnext.tenant_id',true));
-- +goose StatementBegin
CREATE FUNCTION chat_filter_forbid_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'filter history is immutable';
END $$;
-- +goose StatementEnd
CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON chat_filter_version FOR EACH ROW EXECUTE FUNCTION chat_filter_forbid_mutation();
CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON chat_filter_hit FOR EACH ROW EXECUTE FUNCTION chat_filter_forbid_mutation();

-- +goose Down
DROP TABLE chat_filter_hit;
DROP TABLE chat_filter_enablement;
DROP TABLE chat_filter_version;
DROP FUNCTION chat_filter_forbid_mutation();
