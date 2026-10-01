-- AGENT2-003: durable agent delegation grants and per-user revocation epochs.

-- +goose Up

CREATE TABLE agent_delegation_grant (
    tenant_id             uuid        NOT NULL REFERENCES tenant(tenant_id),
    grant_id              text        NOT NULL CHECK (btrim(grant_id) <> ''),
    user_id               text        NOT NULL CHECK (btrim(user_id) <> ''),
    agent_version         text        NOT NULL CHECK (btrim(agent_version) <> ''),
    installation_id       text        NOT NULL CHECK (btrim(installation_id) <> ''),
    task_id               text        NOT NULL CHECK (btrim(task_id) <> ''),
    plan_skill_set_digest text        NOT NULL CHECK (btrim(plan_skill_set_digest) <> ''),
    purpose               text        NOT NULL CHECK (btrim(purpose) <> ''),
    organization_scope_id text        NOT NULL CHECK (btrim(organization_scope_id) <> ''),
    skills                text[]      NOT NULL CHECK (cardinality(skills) > 0),
    skill_scopes          jsonb       NOT NULL CHECK (jsonb_typeof(skill_scopes) = 'object'),
    authority             jsonb       NOT NULL CHECK (jsonb_typeof(authority) = 'object'),
    not_before            timestamptz NOT NULL,
    expires_at            timestamptz NOT NULL,
    revocation_epoch      bigint      NOT NULL CHECK (revocation_epoch >= 1),
    revoked               boolean     NOT NULL DEFAULT false,
    revoked_reason        text,
    revoked_at            timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, grant_id),
    CHECK (expires_at > not_before),
    CHECK (expires_at - not_before <= interval '7 days'),
    CHECK (revoked OR (revoked_reason IS NULL AND revoked_at IS NULL))
);

CREATE INDEX agent_delegation_grant_user ON agent_delegation_grant (tenant_id, user_id);

CREATE TABLE agent_delegation_epoch (
    tenant_id   uuid        NOT NULL REFERENCES tenant(tenant_id),
    user_id     text        NOT NULL CHECK (btrim(user_id) <> ''),
    epoch       bigint      NOT NULL CHECK (epoch >= 1),
    last_reason text,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, user_id)
);

-- +goose StatementBegin
CREATE FUNCTION agent_delegation_grant_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'table % holds durable ceilings; DELETE is forbidden', TG_TABLE_NAME
            USING ERRCODE = '23514';
    END IF;
    -- Revocation is the only legal update: it may set revoked, its reason and
    -- time, and the Revoked flag inside the authority document, never clear them.
    IF (to_jsonb(NEW) - 'revoked' - 'revoked_reason' - 'revoked_at' - 'authority')
        IS DISTINCT FROM (to_jsonb(OLD) - 'revoked' - 'revoked_reason' - 'revoked_at' - 'authority')
       OR (NEW.authority - 'Revoked') IS DISTINCT FROM (OLD.authority - 'Revoked') THEN
        RAISE EXCEPTION 'table % is immutable except for revocation', TG_TABLE_NAME
            USING ERRCODE = '23514';
    END IF;
    IF OLD.revoked AND NOT NEW.revoked THEN
        RAISE EXCEPTION 'a revoked delegation grant cannot be reinstated'
            USING ERRCODE = '23514';
    END IF;
    IF OLD.revoked AND (NEW.revoked_reason IS DISTINCT FROM OLD.revoked_reason
                        OR NEW.revoked_at IS DISTINCT FROM OLD.revoked_at) THEN
        RAISE EXCEPTION 'the original revocation record cannot be rewritten'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION agent_delegation_epoch_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'table % is monotonic; DELETE is forbidden', TG_TABLE_NAME
            USING ERRCODE = '23514';
    END IF;
    IF NEW.epoch < OLD.epoch OR NEW.tenant_id <> OLD.tenant_id OR NEW.user_id <> OLD.user_id THEN
        RAISE EXCEPTION 'revocation epoch must never decrease'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER agent_delegation_grant_guard
    BEFORE UPDATE OR DELETE ON agent_delegation_grant
    FOR EACH ROW EXECUTE FUNCTION agent_delegation_grant_guard();
CREATE TRIGGER agent_delegation_epoch_guard
    BEFORE UPDATE OR DELETE ON agent_delegation_epoch
    FOR EACH ROW EXECUTE FUNCTION agent_delegation_epoch_guard();

ALTER TABLE agent_delegation_grant ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_delegation_grant FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_delegation_grant
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE agent_delegation_epoch ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_delegation_epoch FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_delegation_epoch
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE ON agent_delegation_grant, agent_delegation_epoch TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM agent_delegation_grant) THEN
        RAISE EXCEPTION 'cannot remove retained agent delegation grants';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE agent_delegation_epoch;
DROP TABLE agent_delegation_grant;
DROP FUNCTION agent_delegation_epoch_guard();
DROP FUNCTION agent_delegation_grant_guard();
