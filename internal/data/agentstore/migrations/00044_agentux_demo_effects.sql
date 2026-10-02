-- +goose Up
CREATE TABLE support_effect_reservation (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    skill_id text NOT NULL CHECK (skill_id IN ('support.create_ticket','support.alert_channel')),
    message_id text NOT NULL,
    budget_day date NOT NULL,
    reserved_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, skill_id, message_id),
    FOREIGN KEY (tenant_id,message_id) REFERENCES support_inbox_message(tenant_id,message_id)
);
CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON support_effect_reservation
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
ALTER TABLE support_effect_reservation ENABLE ROW LEVEL SECURITY;
ALTER TABLE support_effect_reservation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON support_effect_reservation
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON support_effect_reservation TO hcmnext_agent_app;

CREATE TABLE support_run_plan (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    message_id text NOT NULL,
    run_id text NOT NULL,
    plan_ref text NOT NULL,
    plan_digest text NOT NULL CHECK (plan_digest ~ '^sha256:[0-9a-f]{64}$'),
    recorded_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id,message_id),
    UNIQUE (tenant_id,run_id),
    FOREIGN KEY (tenant_id,message_id) REFERENCES support_inbox_message(tenant_id,message_id)
);
CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON support_run_plan
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
ALTER TABLE support_run_plan ENABLE ROW LEVEL SECURITY;
ALTER TABLE support_run_plan FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON support_run_plan
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON support_run_plan TO hcmnext_agent_app;

-- +goose Down
DROP TABLE support_run_plan;
DROP TABLE support_effect_reservation;
