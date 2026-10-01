-- AGENT2-014: durable per-tenant hash chain of agent-for-user audit events.

-- +goose Up

CREATE TABLE agent_audit_record (
    tenant_id   uuid        NOT NULL REFERENCES tenant(tenant_id),
    sequence    bigint      NOT NULL CHECK (sequence > 0),
    event_id    text        NOT NULL CHECK (btrim(event_id) <> ''),
    kind        text        NOT NULL CHECK (kind IN ('SKILL_CALL', 'APPROVAL', 'INTENT_ORIGIN', 'WORKFLOW_RUN', 'CONNECTOR_OPERATION', 'MODEL_CALL')),
    user_id     text        NOT NULL CHECK (btrim(user_id) <> ''),
    task_id     text        NOT NULL CHECK (btrim(task_id) <> ''),
    entry       jsonb       NOT NULL CHECK (jsonb_typeof(entry) = 'object'),
    prev_hash   text        NOT NULL,
    chain_hash  text        NOT NULL CHECK (chain_hash ~ '^sha256:[0-9a-f]{64}$'),
    recorded_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, sequence),
    UNIQUE (tenant_id, event_id),
    CHECK ((sequence = 1 AND prev_hash = '') OR (sequence > 1 AND prev_hash ~ '^sha256:[0-9a-f]{64}$'))
);

CREATE TABLE agent_audit_edge (
    tenant_id uuid   NOT NULL REFERENCES tenant(tenant_id),
    sequence  bigint NOT NULL,
    ordinal   integer NOT NULL CHECK (ordinal >= 0),
    kind      text   NOT NULL CHECK (kind IN ('TASK', 'INTENT', 'APPROVAL', 'WORKFLOW_RUN', 'CONNECTOR_OPERATION', 'AUTHORIZED_BY', 'CAUSED')),
    from_ref  text   NOT NULL CHECK (btrim(from_ref) <> ''),
    to_ref    text   NOT NULL CHECK (btrim(to_ref) <> ''),
    PRIMARY KEY (tenant_id, sequence, ordinal),
    FOREIGN KEY (tenant_id, sequence) REFERENCES agent_audit_record(tenant_id, sequence)
);

CREATE INDEX agent_audit_record_task ON agent_audit_record (tenant_id, task_id, sequence);
CREATE INDEX agent_audit_record_user ON agent_audit_record (tenant_id, user_id, sequence);
CREATE INDEX agent_audit_edge_lookup ON agent_audit_edge (tenant_id, kind, from_ref, to_ref);

ALTER TABLE agent_audit_record ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_audit_record FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_audit_record
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE TRIGGER agent_audit_record_immutable
    BEFORE UPDATE OR DELETE ON agent_audit_record
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE agent_audit_edge ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_audit_edge FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_audit_edge
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE TRIGGER agent_audit_edge_immutable
    BEFORE UPDATE OR DELETE ON agent_audit_edge
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

GRANT SELECT, INSERT ON agent_audit_record, agent_audit_edge TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM agent_audit_record) THEN
        RAISE EXCEPTION 'cannot remove retained agent audit records';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE agent_audit_edge;
DROP TABLE agent_audit_record;
