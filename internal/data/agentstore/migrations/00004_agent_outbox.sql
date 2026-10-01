-- Append-only cross-database exchange ledger. Delivery is recorded as a
-- separate receipt, so a crash after publish can be reconciled idempotently.
-- +goose Up

CREATE TABLE agent_outbox (
    tenant_id      uuid        NOT NULL REFERENCES tenant (tenant_id),
    event_id       text        NOT NULL CHECK (btrim(event_id) <> ''),
    aggregate_kind text        NOT NULL CHECK (btrim(aggregate_kind) <> ''),
    aggregate_id   text        NOT NULL CHECK (btrim(aggregate_id) <> ''),
    event_type     text        NOT NULL CHECK (btrim(event_type) <> ''),
    payload        jsonb       NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    occurred_at    timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, event_id)
);
CREATE INDEX agent_outbox_pending ON agent_outbox (tenant_id, occurred_at, event_id);

CREATE TABLE agent_outbox_delivery (
    tenant_id      uuid        NOT NULL,
    event_id       text        NOT NULL,
    destination    text        NOT NULL CHECK (btrim(destination) <> ''),
    delivered_at   timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, event_id, destination),
    FOREIGN KEY (tenant_id, event_id) REFERENCES agent_outbox (tenant_id, event_id)
);

ALTER TABLE agent_outbox ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_outbox FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_outbox
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE TRIGGER agent_outbox_immutable
    BEFORE UPDATE OR DELETE ON agent_outbox
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE agent_outbox_delivery ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_outbox_delivery FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_outbox_delivery
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE TRIGGER agent_outbox_delivery_immutable
    BEFORE UPDATE OR DELETE ON agent_outbox_delivery
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

GRANT SELECT, INSERT ON agent_outbox, agent_outbox_delivery TO hcmnext_agent_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM agent_outbox) THEN
        RAISE EXCEPTION 'cannot remove retained agent outbox entries';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE agent_outbox_delivery;
DROP TABLE agent_outbox;
