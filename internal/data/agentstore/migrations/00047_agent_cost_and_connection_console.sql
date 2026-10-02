-- AGENTCOST-006: owner spend limits, the audit of every change to them, and the
-- finished-run cost ledger the cost report and the limits read.
-- AGENT2-019: the administrator console's revisions of a connection (drafts,
-- approvals, published and replaced revisions) and its audit trail.
-- A run that hits a daily limit is recorded for the asker with its own code.
-- +goose Up
CREATE TABLE agent_spend_limits (
    tenant_id         uuid        NOT NULL REFERENCES tenant(tenant_id),
    agent_id          text        NOT NULL CHECK (btrim(agent_id) <> ''),
    conversation_id   text        NOT NULL DEFAULT '',
    max_runs          bigint      NOT NULL DEFAULT 0 CHECK (max_runs >= 0),
    max_spend_micros  bigint      NOT NULL DEFAULT 0 CHECK (max_spend_micros >= 0),
    updated_by        text        NOT NULL CHECK (btrim(updated_by) <> ''),
    updated_at        timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, agent_id, conversation_id)
);
-- A removed limit is a row with both ceilings at zero, so no DELETE is granted.
ALTER TABLE agent_spend_limits ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_spend_limits FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_spend_limits
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON agent_spend_limits TO hcmnext_agent_app;

CREATE TABLE agent_run_costs (
    tenant_id        uuid        NOT NULL REFERENCES tenant(tenant_id),
    agent_id         text        NOT NULL CHECK (btrim(agent_id) <> ''),
    conversation_id  text        NOT NULL DEFAULT '',
    run_id           text        NOT NULL CHECK (btrim(run_id) <> ''),
    at               timestamptz NOT NULL,
    kind             text        NOT NULL CHECK (kind IN ('answer','screening','decision')),
    spend_micros     bigint      NOT NULL CHECK (spend_micros >= 0),
    answered         boolean     NOT NULL DEFAULT false,
    messages_read    bigint      NOT NULL DEFAULT 0 CHECK (messages_read >= 0),
    PRIMARY KEY (tenant_id, run_id)
);
CREATE INDEX agent_run_costs_by_agent_day ON agent_run_costs (tenant_id, agent_id, at DESC);
CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON agent_run_costs
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
ALTER TABLE agent_run_costs ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_run_costs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_run_costs
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON agent_run_costs TO hcmnext_agent_app;

CREATE TABLE agent_spend_limit_audit (
    id           bigint      GENERATED ALWAYS AS IDENTITY,
    tenant_id    uuid        NOT NULL REFERENCES tenant(tenant_id),
    agent_id     text        NOT NULL CHECK (btrim(agent_id) <> ''),
    actor        text        NOT NULL CHECK (btrim(actor) <> ''),
    at           timestamptz NOT NULL,
    before_json  jsonb,
    after_json   jsonb,
    PRIMARY KEY (tenant_id, id)
);
CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON agent_spend_limit_audit
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
ALTER TABLE agent_spend_limit_audit ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_spend_limit_audit FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_spend_limit_audit
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON agent_spend_limit_audit TO hcmnext_agent_app;

CREATE TABLE agent_connection_drafts (
    tenant_id     uuid        NOT NULL REFERENCES tenant(tenant_id),
    connection_id text        NOT NULL CHECK (btrim(connection_id) <> ''),
    number        bigint      NOT NULL CHECK (number >= 1),
    body          jsonb       NOT NULL CHECK (jsonb_typeof(body) = 'object'),
    status        text        NOT NULL CHECK (status IN ('DRAFT','AWAITING_APPROVAL','APPROVED','PUBLISHED','SUPERSEDED')),
    created_by    text        NOT NULL CHECK (btrim(created_by) <> ''),
    requested_by  text        NOT NULL DEFAULT '',
    approved_by   text        NOT NULL DEFAULT '',
    approved_at   timestamptz,
    step_up       boolean     NOT NULL DEFAULT false,
    digest        text        NOT NULL CHECK (digest LIKE 'sha256:%'),
    published_at  timestamptz,
    created_at    timestamptz NOT NULL,
    updated_at    timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, connection_id, number)
);
-- +goose StatementBegin
CREATE FUNCTION agent_connection_drafts_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'table % is retained; DELETE is forbidden', TG_TABLE_NAME USING ERRCODE = '23514';
    END IF;
    -- A revision that has been live is history: its content never changes, and
    -- the only thing that can still happen to it is being replaced.
    IF OLD.status IN ('PUBLISHED','SUPERSEDED') THEN
        IF NEW.body IS DISTINCT FROM OLD.body OR NEW.digest <> OLD.digest OR NEW.created_by <> OLD.created_by
           OR NEW.approved_by <> OLD.approved_by OR NEW.requested_by <> OLD.requested_by
           OR NEW.status NOT IN (OLD.status, 'SUPERSEDED') OR (OLD.status = 'SUPERSEDED' AND NEW.status <> 'SUPERSEDED') THEN
            RAISE EXCEPTION 'a published connection revision is immutable' USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER agent_connection_drafts_guard BEFORE UPDATE OR DELETE ON agent_connection_drafts
    FOR EACH ROW EXECUTE FUNCTION agent_connection_drafts_guard();
ALTER TABLE agent_connection_drafts ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_connection_drafts FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_connection_drafts
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON agent_connection_drafts TO hcmnext_agent_app;

CREATE TABLE agent_connection_console_audit (
    id          bigint      GENERATED ALWAYS AS IDENTITY,
    tenant_id   uuid        NOT NULL REFERENCES tenant(tenant_id),
    actor       text        NOT NULL CHECK (btrim(actor) <> ''),
    action      text        NOT NULL CHECK (btrim(action) <> ''),
    revision    text        NOT NULL DEFAULT '',
    at          timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, id)
);
CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON agent_connection_console_audit
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
ALTER TABLE agent_connection_console_audit ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_connection_console_audit FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_connection_console_audit
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON agent_connection_console_audit TO hcmnext_agent_app;

-- A run stopped by a daily limit is shown to the person who asked as a limit,
-- which is not retryable until the day ends.
ALTER TABLE persona_invocation_failure DROP CONSTRAINT persona_invocation_failure_failure_code_check;
ALTER TABLE persona_invocation_failure DROP CONSTRAINT persona_invocation_failure_check;
ALTER TABLE persona_invocation_failure ADD CONSTRAINT persona_invocation_failure_failure_code_check
    CHECK (failure_code IN ('MODEL_UNAVAILABLE','OUTPUT_REJECTED','DELIVERY_FAILED','ADMISSION_REFUSED','ADMISSION_UNAVAILABLE','EXECUTION_UNAVAILABLE','INVOCATION_FAILED','ANSWER_INTERRUPTED','DAILY_LIMIT_REACHED'));
ALTER TABLE persona_invocation_failure ADD CONSTRAINT persona_invocation_failure_check
    CHECK (NOT retryable OR failure_code IN ('MODEL_UNAVAILABLE','ADMISSION_UNAVAILABLE','EXECUTION_UNAVAILABLE','ANSWER_INTERRUPTED'));

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM agent_run_costs)
       OR EXISTS (SELECT 1 FROM agent_spend_limit_audit)
       OR EXISTS (SELECT 1 FROM agent_connection_drafts)
       OR EXISTS (SELECT 1 FROM agent_connection_console_audit)
       OR EXISTS (SELECT 1 FROM persona_invocation_failure WHERE failure_code = 'DAILY_LIMIT_REACHED') THEN
        RAISE EXCEPTION 'cannot remove retained agent cost and connection console evidence';
    END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE persona_invocation_failure DROP CONSTRAINT persona_invocation_failure_check;
ALTER TABLE persona_invocation_failure DROP CONSTRAINT persona_invocation_failure_failure_code_check;
ALTER TABLE persona_invocation_failure ADD CONSTRAINT persona_invocation_failure_failure_code_check
    CHECK (failure_code IN ('MODEL_UNAVAILABLE','OUTPUT_REJECTED','DELIVERY_FAILED','ADMISSION_REFUSED','ADMISSION_UNAVAILABLE','EXECUTION_UNAVAILABLE','INVOCATION_FAILED','ANSWER_INTERRUPTED'));
ALTER TABLE persona_invocation_failure ADD CONSTRAINT persona_invocation_failure_check
    CHECK (NOT retryable OR failure_code IN ('MODEL_UNAVAILABLE','ADMISSION_UNAVAILABLE','EXECUTION_UNAVAILABLE','ANSWER_INTERRUPTED'));
DROP TABLE agent_connection_console_audit;
DROP TABLE agent_connection_drafts;
DROP FUNCTION agent_connection_drafts_guard();
DROP TABLE agent_spend_limit_audit;
DROP TABLE agent_run_costs;
DROP TABLE agent_spend_limits;
