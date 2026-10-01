-- AGENT2-005: durable, tenant-scoped current administrator grants for exact
-- skill versions. Grant creation authorization is provided by its owning
-- administration surface; this table retains the reviewed grant evidence.

-- +goose Up

CREATE TABLE agent_skill_grant (
    tenant_id            uuid        NOT NULL REFERENCES tenant(tenant_id),
    grant_id             text        NOT NULL CHECK (btrim(grant_id) <> ''),
    skill_id             text        NOT NULL CHECK (btrim(skill_id) <> ''),
    skill_version        bigint      NOT NULL CHECK (skill_version > 0),
    roles                text[]      NOT NULL CHECK (cardinality(roles) > 0),
    population           text        NOT NULL CHECK (btrim(population) <> ''),
    organization_scopes  text[]      NOT NULL CHECK (cardinality(organization_scopes) > 0),
    purposes             text[]      NOT NULL CHECK (cardinality(purposes) > 0),
    consent_required     boolean     NOT NULL DEFAULT false,
    not_before           timestamptz NOT NULL,
    expires_at           timestamptz,
    granted_by           text        NOT NULL CHECK (btrim(granted_by) <> ''),
    granted_at           timestamptz NOT NULL,
    admin_evidence_ref   text        NOT NULL CHECK (btrim(admin_evidence_ref) <> ''),
    revoked_at           timestamptz,
    revoked_by           text,
    revoked_reason       text,
    PRIMARY KEY (tenant_id, grant_id),
    CHECK (expires_at IS NULL OR expires_at > not_before),
    CHECK ((revoked_at IS NULL) = (revoked_by IS NULL)
       AND (revoked_at IS NULL) = (revoked_reason IS NULL))
);

CREATE INDEX agent_skill_grant_lookup
    ON agent_skill_grant (tenant_id, skill_id, skill_version, grant_id)
    WHERE revoked_at IS NULL;

-- +goose StatementBegin
CREATE FUNCTION agent_skill_grant_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'table % retains grant evidence; DELETE is forbidden', TG_TABLE_NAME
            USING ERRCODE = '23514';
    END IF;
    IF (to_jsonb(NEW) - 'revoked_at' - 'revoked_by' - 'revoked_reason')
        IS DISTINCT FROM (to_jsonb(OLD) - 'revoked_at' - 'revoked_by' - 'revoked_reason') THEN
        RAISE EXCEPTION 'table % is immutable except for revocation', TG_TABLE_NAME
            USING ERRCODE = '23514';
    END IF;
    IF OLD.revoked_at IS NOT NULL AND
       (NEW.revoked_at IS DISTINCT FROM OLD.revoked_at
        OR NEW.revoked_by IS DISTINCT FROM OLD.revoked_by
        OR NEW.revoked_reason IS DISTINCT FROM OLD.revoked_reason) THEN
        RAISE EXCEPTION 'a revoked skill grant cannot be reinstated or rewritten'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER agent_skill_grant_guard
    BEFORE UPDATE OR DELETE ON agent_skill_grant
    FOR EACH ROW EXECUTE FUNCTION agent_skill_grant_guard();

ALTER TABLE agent_skill_grant ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_skill_grant FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_skill_grant
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE ON agent_skill_grant TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM agent_skill_grant) THEN
        RAISE EXCEPTION 'cannot remove retained agent skill grants';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE agent_skill_grant;
DROP FUNCTION agent_skill_grant_guard();
