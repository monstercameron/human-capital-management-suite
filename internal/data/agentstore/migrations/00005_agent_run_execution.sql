-- AGENT-016: durable execution state is separate from admission and effect
-- evidence, so uncertain capability outcomes cannot be replayed as checkpoints.
-- +goose Up

CREATE TABLE agent_run_execution (
    tenant_id       uuid        NOT NULL REFERENCES tenant (tenant_id),
    run_id          text        NOT NULL CHECK (btrim(run_id) <> ''),
    admission_id    text        NOT NULL CHECK (btrim(admission_id) <> ''),
    request_digest  char(64)    NOT NULL CHECK (request_digest ~ '^[0-9a-f]{64}$'),
    agent_id        text        NOT NULL CHECK (btrim(agent_id) <> ''),
    agent_version   text        NOT NULL CHECK (btrim(agent_version) <> ''),
    agent_digest    text        NOT NULL CHECK (agent_digest ~ '^sha256:[0-9a-f]{64}$'),
    context_digest  text        NOT NULL CHECK (context_digest ~ '^sha256:[0-9a-f]{64}$'),
    deadline        timestamptz NOT NULL,
    state           text        NOT NULL CHECK (state IN ('READY','RUNNING','WAITING','RECONCILING','COMPLETED','FAILED','CANCELLED','EXPIRED','NEEDS_REPAIR')),
    revision        bigint      NOT NULL CHECK (revision > 0),
    fence           bigint      NOT NULL CHECK (fence >= 0),
    lease_owner     text,
    lease_until     timestamptz,
    cancel_requested boolean    NOT NULL DEFAULT false,
    expire_requested boolean    NOT NULL DEFAULT false,
    failure_requested boolean   NOT NULL DEFAULT false,
    terminal_code  text,
    created_at      timestamptz NOT NULL,
    updated_at      timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, run_id),
    UNIQUE (tenant_id, admission_id),
    FOREIGN KEY (tenant_id, admission_id) REFERENCES agent_run_request (tenant_id, request_id),
    CHECK ((state = 'RUNNING') = (lease_owner IS NOT NULL AND lease_until IS NOT NULL)),
    CHECK (terminal_code IS NULL OR btrim(terminal_code) <> '')
);

-- The execution journal may be created only for a committed accepted
-- admission. Rejected requests remain immutable records with no execution.
-- +goose StatementBegin
CREATE FUNCTION agent_run_execution_require_accepted() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE admitted_decision text;
BEGIN
    SELECT decision INTO admitted_decision FROM agent_run_request
    WHERE tenant_id = NEW.tenant_id AND request_id = NEW.admission_id;
    IF admitted_decision IS DISTINCT FROM 'ACCEPTED' THEN
        RAISE EXCEPTION 'agent execution requires accepted admission';
    END IF;
    RETURN NEW;
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION agent_run_execution_require_accepted() FROM PUBLIC;
CREATE TRIGGER agent_run_execution_admission
    BEFORE INSERT ON agent_run_execution
    FOR EACH ROW EXECUTE FUNCTION agent_run_execution_require_accepted();
CREATE INDEX agent_run_execution_recovery ON agent_run_execution (tenant_id, state, lease_until, deadline);

CREATE TABLE agent_run_checkpoint (
    tenant_id   uuid        NOT NULL,
    run_id      text        NOT NULL,
    sequence    bigint      NOT NULL CHECK (sequence > 0),
    phase       text        NOT NULL CHECK (phase IN ('ADMISSION','CONTEXT','MODEL_CALL','TOOL_CALL','VALIDATION','DELIVERY')),
    attempt     integer     NOT NULL CHECK (attempt >= 0),
    ref         text,
    digest      text,
    occurred_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, run_id, sequence),
    FOREIGN KEY (tenant_id, run_id) REFERENCES agent_run_execution (tenant_id, run_id),
    CHECK (ref IS NULL OR btrim(ref) <> ''),
    CHECK (digest IS NULL OR digest ~ '^sha256:[0-9a-f]{64}$'),
    CHECK (ref IS NOT NULL OR digest IS NOT NULL)
);
CREATE INDEX agent_run_checkpoint_phase ON agent_run_checkpoint (tenant_id, run_id, phase, sequence);
CREATE TRIGGER agent_run_checkpoint_immutable
    BEFORE UPDATE OR DELETE ON agent_run_checkpoint
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

CREATE TABLE agent_run_effect (
    tenant_id        uuid        NOT NULL,
    run_id           text        NOT NULL,
    effect_id        text        NOT NULL CHECK (btrim(effect_id) <> ''),
    idempotency_key  text        NOT NULL CHECK (btrim(idempotency_key) <> ''),
    arguments_digest text        NOT NULL CHECK (arguments_digest ~ '^sha256:[0-9a-f]{64}$'),
    status           text        NOT NULL CHECK (status IN ('UNKNOWN','APPLIED','NOT_APPLIED')),
    result_ref       text,
    result_digest    text,
    started_at       timestamptz NOT NULL,
    resolved_at      timestamptz,
    PRIMARY KEY (tenant_id, run_id, effect_id),
    UNIQUE (tenant_id, run_id, idempotency_key),
    FOREIGN KEY (tenant_id, run_id) REFERENCES agent_run_execution (tenant_id, run_id),
    CHECK ((status = 'UNKNOWN' AND resolved_at IS NULL) OR (status <> 'UNKNOWN' AND resolved_at IS NOT NULL)),
    CHECK (result_ref IS NULL OR btrim(result_ref) <> ''),
    CHECK (result_digest IS NULL OR result_digest ~ '^sha256:[0-9a-f]{64}$'),
    CHECK (status <> 'APPLIED' OR (result_ref IS NOT NULL AND result_digest IS NOT NULL))
);
CREATE INDEX agent_run_effect_unresolved ON agent_run_effect (tenant_id, run_id, status);

ALTER TABLE agent_run_execution ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_run_execution FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_run_execution
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE agent_run_checkpoint ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_run_checkpoint FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_run_checkpoint
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE agent_run_effect ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_run_effect FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_run_effect
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE ON agent_run_execution, agent_run_effect TO hcmnext_agent_app;
GRANT SELECT, INSERT ON agent_run_checkpoint TO hcmnext_agent_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM agent_run_execution) THEN
        RAISE EXCEPTION 'cannot remove retained agent execution state';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE agent_run_effect;
DROP TABLE agent_run_checkpoint;
DROP TABLE agent_run_execution;
DROP FUNCTION agent_run_execution_require_accepted();
