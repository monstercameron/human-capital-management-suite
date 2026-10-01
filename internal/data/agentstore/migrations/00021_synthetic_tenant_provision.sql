-- AGENT2-025: explicit synthetic evaluation tenant provisions.
-- +goose Up

-- +goose StatementBegin
DO $$
BEGIN
    CREATE ROLE hcmnext_agent_eval_provisioner
        NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
        NOLOGIN NOREPLICATION NOBYPASSRLS;
EXCEPTION
    WHEN duplicate_object OR unique_violation THEN NULL;
END
$$;
-- +goose StatementEnd

ALTER ROLE hcmnext_agent_eval_provisioner
    NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOLOGIN NOREPLICATION NOBYPASSRLS;

-- +goose StatementBegin
DO $$
DECLARE target_schema text := current_schema();
BEGIN
    EXECUTE format('GRANT USAGE ON SCHEMA %I TO hcmnext_agent_eval_provisioner', target_schema);
END
$$;
-- +goose StatementEnd

CREATE TABLE synthetic_tenant_provision (
    tenant_id          uuid        NOT NULL REFERENCES tenant (tenant_id),
    suite_tenant_id    text        NOT NULL CHECK (btrim(suite_tenant_id) <> ''),
    provision_id       text        NOT NULL CHECK (btrim(provision_id) <> ''),
    marker_id          text        NOT NULL CHECK (btrim(marker_id) <> ''),
    purpose            text        NOT NULL CHECK (purpose = 'agent-evaluation'),
    status             text        NOT NULL CHECK (status IN ('ACTIVE','REVOKED')),
    expires_at         timestamptz NOT NULL,
    grant_store_id     text        NOT NULL CHECK (btrim(grant_store_id) <> ''),
    task_store_id      text        NOT NULL CHECK (btrim(task_store_id) <> ''),
    budget_ledger_id   text        NOT NULL CHECK (btrim(budget_ledger_id) <> ''),
    audit_store_id     text        NOT NULL CHECK (btrim(audit_store_id) <> ''),
    tool_owner_id      text        NOT NULL CHECK (btrim(tool_owner_id) <> ''),
    tool_owner_profile text        NOT NULL CHECK (tool_owner_profile = 'fixture-only/v1'),
    issued_at          timestamptz NOT NULL,
    revoked_at         timestamptz,
    revoke_reason      text        NOT NULL DEFAULT '',
    PRIMARY KEY (tenant_id, provision_id),
    UNIQUE (tenant_id, marker_id),
    CHECK (grant_store_id <> task_store_id AND grant_store_id <> budget_ledger_id AND
        grant_store_id <> audit_store_id AND grant_store_id <> tool_owner_id AND
        task_store_id <> budget_ledger_id AND task_store_id <> audit_store_id AND
        task_store_id <> tool_owner_id AND budget_ledger_id <> audit_store_id AND
        budget_ledger_id <> tool_owner_id AND audit_store_id <> tool_owner_id),
    CHECK ((status = 'ACTIVE' AND revoked_at IS NULL AND revoke_reason = '') OR
        (status = 'REVOKED' AND revoked_at IS NOT NULL AND btrim(revoke_reason) <> '')),
    CHECK (expires_at > issued_at)
);
CREATE INDEX synthetic_tenant_provision_current ON synthetic_tenant_provision (tenant_id, status, expires_at);
CREATE UNIQUE INDEX synthetic_tenant_provision_one_active ON synthetic_tenant_provision (tenant_id, suite_tenant_id) WHERE status = 'ACTIVE';
ALTER TABLE synthetic_tenant_provision ENABLE ROW LEVEL SECURITY;
ALTER TABLE synthetic_tenant_provision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON synthetic_tenant_provision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT ON synthetic_tenant_provision TO hcmnext_agent_app;
GRANT SELECT, INSERT ON synthetic_tenant_provision TO hcmnext_agent_eval_provisioner;
GRANT UPDATE (status, revoked_at, revoke_reason)
    ON synthetic_tenant_provision TO hcmnext_agent_eval_provisioner;

CREATE TABLE synthetic_tenant_provision_audit (
    tenant_id    uuid        NOT NULL,
    provision_id text        NOT NULL,
    action       text        NOT NULL CHECK (action IN ('ISSUED','REVOKED')),
    actor_id     text        NOT NULL CHECK (btrim(actor_id) <> ''),
    occurred_at  timestamptz NOT NULL,
    reason       text        NOT NULL DEFAULT '',
    PRIMARY KEY (tenant_id, provision_id, action),
    FOREIGN KEY (tenant_id, provision_id)
        REFERENCES synthetic_tenant_provision (tenant_id, provision_id),
    CHECK ((action = 'ISSUED' AND reason = '') OR (action = 'REVOKED' AND btrim(reason) <> ''))
);
ALTER TABLE synthetic_tenant_provision_audit ENABLE ROW LEVEL SECURITY;
ALTER TABLE synthetic_tenant_provision_audit FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON synthetic_tenant_provision_audit
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE TRIGGER synthetic_tenant_provision_audit_immutable
    BEFORE UPDATE OR DELETE ON synthetic_tenant_provision_audit
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
GRANT SELECT ON synthetic_tenant_provision_audit TO hcmnext_agent_app;
GRANT SELECT, INSERT ON synthetic_tenant_provision_audit TO hcmnext_agent_eval_provisioner;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM synthetic_tenant_provision_audit)
       OR EXISTS (SELECT 1 FROM synthetic_tenant_provision) THEN
        RAISE EXCEPTION 'cannot remove retained synthetic tenant provisions';
    END IF;
END $$;
-- +goose StatementEnd
DROP TRIGGER synthetic_tenant_provision_audit_immutable ON synthetic_tenant_provision_audit;
REVOKE ALL PRIVILEGES ON synthetic_tenant_provision, synthetic_tenant_provision_audit FROM hcmnext_agent_eval_provisioner;
-- +goose StatementBegin
DO $$
DECLARE target_schema text := current_schema();
BEGIN
    EXECUTE format('REVOKE USAGE ON SCHEMA %I FROM hcmnext_agent_eval_provisioner', target_schema);
END
$$;
-- +goose StatementEnd
DROP TABLE synthetic_tenant_provision_audit;
DROP TABLE synthetic_tenant_provision;
DROP ROLE hcmnext_agent_eval_provisioner;
