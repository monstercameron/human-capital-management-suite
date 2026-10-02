-- +goose Up
CREATE TABLE agent_announcement (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    tenant_key text NOT NULL CHECK (btrim(tenant_key) <> ''),
    announcement_id text NOT NULL CHECK (btrim(announcement_id) = announcement_id AND char_length(announcement_id) BETWEEN 1 AND 128),
    installation_id text NOT NULL CHECK (btrim(installation_id) <> ''),
    persona_id text NOT NULL CHECK (btrim(persona_id) <> ''),
    conversation_id text NOT NULL CHECK (btrim(conversation_id) <> ''),
    instruction text NOT NULL CHECK (char_length(instruction) BETWEEN 1 AND 1000 AND btrim(instruction) <> ''),
    document_references jsonb NOT NULL CHECK (jsonb_typeof(document_references) = 'array' AND jsonb_array_length(document_references) BETWEEN 1 AND 5),
    cadence text NOT NULL CHECK (cadence IN ('NOW','ONCE','DAILY','WEEKLY','MONTHLY')),
    weekdays smallint[] NOT NULL DEFAULT '{}' CHECK (weekdays <@ ARRAY[0,1,2,3,4,5,6]::smallint[]),
    month_day smallint NOT NULL DEFAULT 0 CHECK (month_day BETWEEN 0 AND 31),
    local_time time NOT NULL,
    zone text NOT NULL CHECK (btrim(zone) <> ''),
    state text NOT NULL CHECK (state IN ('ACTIVE','PAUSED','DELETED')),
    owner_id text NOT NULL CHECK (btrim(owner_id) <> ''),
    scheduler_id text NOT NULL CHECK (btrim(scheduler_id) <> ''),
    revision bigint NOT NULL CHECK (revision > 0),
    next_run_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, announcement_id),
    UNIQUE (tenant_id, scheduler_id)
);
ALTER TABLE agent_announcement ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_announcement FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_announcement
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON agent_announcement TO hcmnext_agent_app;

CREATE TABLE agent_announcement_occurrence (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    announcement_id text NOT NULL,
    occurrence_id text NOT NULL CHECK (btrim(occurrence_id) <> ''),
    result text NOT NULL CHECK (result IN ('POSTED','REFUSED','FAILED')),
    reason text NOT NULL DEFAULT '' CHECK (char_length(reason) <= 500),
    message_id text NOT NULL DEFAULT '' CHECK (char_length(message_id) <= 256),
    attempted_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, announcement_id, occurrence_id),
    FOREIGN KEY (tenant_id, announcement_id) REFERENCES agent_announcement(tenant_id, announcement_id)
);
CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON agent_announcement_occurrence
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
ALTER TABLE agent_announcement_occurrence ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_announcement_occurrence FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_announcement_occurrence
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON agent_announcement_occurrence TO hcmnext_agent_app;

CREATE TABLE agent_announcement_command (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    owner_id text NOT NULL,
    idempotency_key text NOT NULL CHECK (char_length(idempotency_key) BETWEEN 8 AND 128),
    announcement_id text NOT NULL,
    request_digest text NOT NULL,
    PRIMARY KEY (tenant_id, owner_id, idempotency_key),
    FOREIGN KEY (tenant_id, announcement_id) REFERENCES agent_announcement(tenant_id, announcement_id)
);
CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON agent_announcement_command
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
ALTER TABLE agent_announcement_command ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_announcement_command FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_announcement_command
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON agent_announcement_command TO hcmnext_agent_app;

-- +goose Down
DROP TABLE agent_announcement_command;
DROP TABLE agent_announcement_occurrence;
DROP TABLE agent_announcement;
