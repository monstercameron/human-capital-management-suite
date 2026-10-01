-- AGENT2-029: authoritative, tenant-scoped current home-organization facts.
-- Home organization is identity context, not a role visibility grant.

-- +goose Up

CREATE TABLE agent_current_home_organization (
    tenant_id             uuid        NOT NULL REFERENCES tenant(tenant_id),
    fact_id               uuid        NOT NULL,
    subject_ref           text        NOT NULL CHECK (btrim(subject_ref) <> ''),
    organization_scope_id text        NOT NULL CHECK (btrim(organization_scope_id) <> ''),
    source_identity       text        NOT NULL CHECK (btrim(source_identity) <> ''),
    revision              bigint      NOT NULL CHECK (revision > 0),
    effective_from        timestamptz NOT NULL,
    effective_until       timestamptz,
    recorded_at           timestamptz NOT NULL DEFAULT now(),
    revoked_at            timestamptz,
    revoked_by            text,
    revocation_reason     text,
    superseded_at         timestamptz,
    superseded_by         uuid,
    PRIMARY KEY (tenant_id, fact_id),
    CHECK (effective_until IS NULL OR effective_until > effective_from),
    CHECK ((revoked_at IS NULL) = (revoked_by IS NULL)
       AND (revoked_at IS NULL) = (revocation_reason IS NULL)),
    CHECK ((superseded_at IS NULL) = (superseded_by IS NULL)),
    CHECK (NOT (revoked_at IS NOT NULL AND superseded_at IS NOT NULL)),
    CHECK (revoked_at IS NULL OR revoked_at >= recorded_at),
    CHECK (superseded_at IS NULL OR superseded_at >= recorded_at)
);

CREATE UNIQUE INDEX agent_current_home_organization_live
    ON agent_current_home_organization (tenant_id, subject_ref)
    WHERE revoked_at IS NULL AND superseded_at IS NULL;
CREATE INDEX agent_current_home_organization_lookup
    ON agent_current_home_organization (tenant_id, subject_ref, effective_from, revision);

-- Identity, organization, source, revision and effective-window fields are
-- immutable. Authority is closed only by a revocation or supersession.
-- +goose StatementBegin
CREATE FUNCTION agent_current_home_organization_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'table % retains home-organization authority; DELETE is forbidden', TG_TABLE_NAME
            USING ERRCODE = '23514';
    END IF;
    IF (to_jsonb(NEW) - 'revoked_at' - 'revoked_by' - 'revocation_reason'
        - 'superseded_at' - 'superseded_by')
       IS DISTINCT FROM
       (to_jsonb(OLD) - 'revoked_at' - 'revoked_by' - 'revocation_reason'
        - 'superseded_at' - 'superseded_by') THEN
        RAISE EXCEPTION 'table % is immutable except for revocation or supersession', TG_TABLE_NAME
            USING ERRCODE = '23514';
    END IF;
    IF OLD.revoked_at IS NOT NULL OR OLD.superseded_at IS NOT NULL THEN
        RAISE EXCEPTION 'a closed home-organization fact cannot be rewritten'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.revoked_at IS NOT NULL AND NEW.superseded_at IS NOT NULL THEN
        RAISE EXCEPTION 'a home-organization fact cannot be both revoked and superseded'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER agent_current_home_organization_guard
    BEFORE UPDATE OR DELETE ON agent_current_home_organization
    FOR EACH ROW EXECUTE FUNCTION agent_current_home_organization_guard();

ALTER TABLE agent_current_home_organization ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_current_home_organization FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_current_home_organization
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

REVOKE DELETE ON agent_current_home_organization FROM PUBLIC, hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON agent_current_home_organization TO hcmnext_app;

-- +goose Down
REVOKE ALL ON agent_current_home_organization FROM hcmnext_app;
DROP POLICY tenant_isolation ON agent_current_home_organization;
ALTER TABLE agent_current_home_organization NO FORCE ROW LEVEL SECURITY;
ALTER TABLE agent_current_home_organization DISABLE ROW LEVEL SECURITY;
DROP TRIGGER agent_current_home_organization_guard ON agent_current_home_organization;
DROP TABLE agent_current_home_organization;
DROP FUNCTION agent_current_home_organization_guard();
