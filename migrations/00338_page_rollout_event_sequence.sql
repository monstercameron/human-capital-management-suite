-- Owner: product experience (REV-067-02). Page rollout history includes
-- rollback events, whose target revision may be older than the previous event.
--
-- +goose Up

CREATE TABLE page_rollout_event (
    tenant_id uuid NOT NULL REFERENCES tenant (tenant_id),
    page_id text NOT NULL CHECK (page_id <> ''),
    record_version bigint NOT NULL CHECK (record_version > 0),
    target_version bigint NOT NULL CHECK (target_version > 0),
    digest char(64) NOT NULL CHECK (digest ~ '^[0-9a-f]{64}$'),
    payload jsonb NOT NULL,
    legacy boolean NOT NULL DEFAULT false,
    PRIMARY KEY (tenant_id, page_id, record_version),
    FOREIGN KEY (tenant_id, page_id, target_version, digest)
        REFERENCES page_definition_revision (tenant_id, page_id, version, digest),
    CHECK (payload->'rollout'->>'page' = page_id),
    CHECK (legacy OR (payload->'rollout'->>'record_version')::bigint = record_version),
    CHECK ((payload->'rollout'->>'version')::bigint = target_version),
    CHECK (payload->'rollout'->>'digest' = digest::text),
    CHECK (payload->>'integrity_digest' ~ '^[0-9a-f]{64}$')
);

-- Preserve rollout rows written under 00332. Their immutable payloads predate
-- record_version, so the explicit legacy bit lets recovery bind the row key
-- while still verifying the original integrity digest unchanged.
INSERT INTO page_rollout_event (tenant_id,page_id,record_version,target_version,digest,payload,legacy)
SELECT tenant_id,page_id,version,version,digest,payload,true FROM page_rollout;

ALTER TABLE page_rollout_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE page_rollout_event FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON page_rollout_event
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE TRIGGER page_rollout_event_append_only BEFORE UPDATE OR DELETE ON page_rollout_event FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
GRANT SELECT, INSERT ON page_rollout_event TO hcmnext_app;

CREATE TABLE page_retirement (
    tenant_id uuid NOT NULL REFERENCES tenant (tenant_id),
    page_id text NOT NULL CHECK (page_id <> ''),
    digest char(64) NOT NULL CHECK (digest ~ '^[0-9a-f]{64}$'),
    payload jsonb NOT NULL,
    PRIMARY KEY (tenant_id, page_id),
    CHECK (payload->'retirement'->>'page' = page_id),
    CHECK (payload->'retirement'->>'reason' <> ''),
    CHECK ((payload->'retirement'->>'effective_from')::bigint >= 0),
    CHECK (payload->>'integrity_digest' = digest::text)
);

ALTER TABLE page_retirement ENABLE ROW LEVEL SECURITY;
ALTER TABLE page_retirement FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON page_retirement
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE TRIGGER page_retirement_append_only BEFORE UPDATE OR DELETE ON page_retirement FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
GRANT SELECT, INSERT ON page_retirement TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00338 is irreversible: rollout events are append-only publication evidence'; END $$;
-- +goose StatementEnd
