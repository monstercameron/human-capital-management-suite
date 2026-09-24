-- Owner: product experience (REV-067-02). Phase: production frontend.
-- storage-disposition: published page revisions and staged rollouts | append-only tenant ledger | local PostgreSQL | tenant-local ACID | product studio governed.
--
-- +goose Up

CREATE TABLE IF NOT EXISTS page_definition_revision (
    tenant_id uuid NOT NULL REFERENCES tenant (tenant_id),
    page_id text NOT NULL CHECK (page_id <> ''),
    version bigint NOT NULL CHECK (version > 0),
    digest char(64) NOT NULL CHECK (digest ~ '^[0-9a-f]{64}$'),
    payload jsonb NOT NULL,
    PRIMARY KEY (tenant_id, page_id, version),
    UNIQUE (tenant_id, page_id, version, digest),
    CHECK (payload->>'digest' = digest::text),
    CHECK (payload->'snapshot'->>'page' = page_id),
    CHECK ((payload->>'version')::bigint = version)
);

CREATE TABLE IF NOT EXISTS page_rollout (
    tenant_id uuid NOT NULL REFERENCES tenant (tenant_id),
    page_id text NOT NULL CHECK (page_id <> ''),
    version bigint NOT NULL CHECK (version > 0),
    digest char(64) NOT NULL CHECK (digest ~ '^[0-9a-f]{64}$'),
    payload jsonb NOT NULL,
    PRIMARY KEY (tenant_id, page_id, version),
    FOREIGN KEY (tenant_id, page_id, version, digest)
        REFERENCES page_definition_revision (tenant_id, page_id, version, digest),
    CHECK (payload->'rollout'->>'page' = page_id),
    CHECK ((payload->'rollout'->>'version')::bigint = version),
    CHECK (payload->'rollout'->>'digest' = digest::text),
    CHECK (payload->>'integrity_digest' ~ '^[0-9a-f]{64}$')
);

ALTER TABLE page_definition_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE page_definition_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON page_definition_revision USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE TRIGGER page_definition_revision_append_only BEFORE UPDATE OR DELETE ON page_definition_revision FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
GRANT SELECT, INSERT ON page_definition_revision TO hcmnext_app;

ALTER TABLE page_rollout ENABLE ROW LEVEL SECURITY;
ALTER TABLE page_rollout FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON page_rollout USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE TRIGGER page_rollout_append_only BEFORE UPDATE OR DELETE ON page_rollout FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
GRANT SELECT, INSERT ON page_rollout TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00333 is irreversible: published page revisions and rollouts are append-only publication evidence'; END $$;
-- +goose StatementEnd
