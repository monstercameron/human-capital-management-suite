-- AGENT2-020 / AGENTP-016 durable persona authority leases and cross-process step fencing.
-- +goose Up

CREATE TABLE persona_security_scope (
    tenant_id   uuid        NOT NULL REFERENCES tenant (tenant_id),
    scope_kind  text        NOT NULL CHECK (scope_kind IN ('TENANT','PRINCIPAL','PERSONA','VERSION','INSTALLATION','RUN')),
    scope_key   text        NOT NULL CHECK (btrim(scope_key) <> ''),
    epoch       bigint      NOT NULL DEFAULT 1 CHECK (epoch > 0),
    state       text        NOT NULL DEFAULT 'ACTIVE' CHECK (state IN ('ACTIVE','REVOKED')),
    reason      text        NOT NULL DEFAULT '',
    revoked_at  timestamptz,
    PRIMARY KEY (tenant_id, scope_kind, scope_key),
    CHECK ((state = 'ACTIVE' AND reason = '' AND revoked_at IS NULL)
        OR (state = 'REVOKED' AND btrim(reason) <> '' AND revoked_at IS NOT NULL))
);
ALTER TABLE persona_security_scope ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_security_scope FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_security_scope
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON persona_security_scope TO hcmnext_agent_app;

CREATE TABLE persona_security_lease (
    tenant_id           uuid        NOT NULL REFERENCES tenant (tenant_id),
    lease_id            text        NOT NULL CHECK (btrim(lease_id) <> ''),
    admission_id        text        NOT NULL,
    invocation_id       text        NOT NULL,
    run_id               text        NOT NULL,
    issuer_id            text        NOT NULL CHECK (btrim(issuer_id) <> ''),
    authority_ref        text        NOT NULL CHECK (btrim(authority_ref) <> ''),
    policy_digest        text        NOT NULL CHECK (policy_digest ~ '^sha256:[0-9a-f]{64}$'),
    principal_id         text        NOT NULL CHECK (btrim(principal_id) <> ''),
    persona_id           text        NOT NULL CHECK (btrim(persona_id) <> ''),
    persona_version      text        NOT NULL CHECK (btrim(persona_version) <> ''),
    installation_id      text        NOT NULL CHECK (btrim(installation_id) <> ''),
    tenant_epoch         bigint      NOT NULL CHECK (tenant_epoch > 0),
    principal_epoch      bigint      NOT NULL CHECK (principal_epoch > 0),
    persona_epoch        bigint      NOT NULL CHECK (persona_epoch > 0),
    version_epoch        bigint      NOT NULL CHECK (version_epoch > 0),
    installation_epoch  bigint      NOT NULL CHECK (installation_epoch > 0),
    run_epoch            bigint      NOT NULL CHECK (run_epoch > 0),
    issued_at            timestamptz NOT NULL,
    expires_at           timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, lease_id),
    UNIQUE (tenant_id, admission_id),
    UNIQUE (tenant_id, run_id),
    FOREIGN KEY (tenant_id, admission_id) REFERENCES agent_run_request (tenant_id, request_id),
    FOREIGN KEY (tenant_id, run_id) REFERENCES agent_run_execution (tenant_id, run_id),
    FOREIGN KEY (tenant_id, invocation_id) REFERENCES persona_invocations (tenant_id, invocation_id),
    CHECK (expires_at > issued_at)
);
CREATE INDEX persona_security_lease_expiry ON persona_security_lease (tenant_id, expires_at, lease_id);
CREATE TRIGGER persona_security_lease_immutable
    BEFORE UPDATE OR DELETE ON persona_security_lease
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
ALTER TABLE persona_security_lease ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_security_lease FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_security_lease
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON persona_security_lease TO hcmnext_agent_app;

CREATE TABLE persona_security_step (
    tenant_id   uuid        NOT NULL,
    lease_id    text        NOT NULL,
    step_id     text        NOT NULL CHECK (btrim(step_id) <> ''),
    state       text        NOT NULL CHECK (state IN ('STARTED','COMPLETED','FAILED','INTERRUPTED')),
    started_at  timestamptz NOT NULL,
    finished_at timestamptz,
    PRIMARY KEY (tenant_id, lease_id, step_id),
    FOREIGN KEY (tenant_id, lease_id) REFERENCES persona_security_lease (tenant_id, lease_id),
    CHECK ((state = 'STARTED' AND finished_at IS NULL) OR (state <> 'STARTED' AND finished_at IS NOT NULL))
);
CREATE INDEX persona_security_step_recovery ON persona_security_step (tenant_id, lease_id, state, started_at);
ALTER TABLE persona_security_step ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_security_step FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_security_step
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE TRIGGER persona_security_step_immutable
    BEFORE DELETE ON persona_security_step
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
-- Only the state and terminal timestamp can be updated, and only once from STARTED.
-- +goose StatementBegin
CREATE FUNCTION persona_security_step_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.tenant_id <> OLD.tenant_id OR NEW.lease_id <> OLD.lease_id
       OR NEW.step_id <> OLD.step_id OR NEW.started_at <> OLD.started_at
       OR OLD.state <> 'STARTED' OR NEW.state NOT IN ('COMPLETED','FAILED','INTERRUPTED')
       OR NEW.finished_at IS NULL THEN
        RAISE EXCEPTION 'persona security step is immutable outside its terminal transition';
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION persona_security_step_guard() FROM PUBLIC;
CREATE TRIGGER persona_security_step_update_guard BEFORE UPDATE ON persona_security_step
    FOR EACH ROW EXECUTE FUNCTION persona_security_step_guard();
GRANT SELECT, INSERT, UPDATE ON persona_security_step TO hcmnext_agent_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM persona_security_step)
       OR EXISTS (SELECT 1 FROM persona_security_lease)
       OR EXISTS (SELECT 1 FROM persona_security_scope WHERE state = 'REVOKED') THEN
        RAISE EXCEPTION 'cannot remove retained persona security state';
    END IF;
END $$;
-- +goose StatementEnd
DROP TRIGGER persona_security_step_update_guard ON persona_security_step;
DROP FUNCTION persona_security_step_guard();
DROP TRIGGER persona_security_step_immutable ON persona_security_step;
DROP TABLE persona_security_step;
DROP TRIGGER persona_security_lease_immutable ON persona_security_lease;
DROP TABLE persona_security_lease;
DROP TABLE persona_security_scope;
