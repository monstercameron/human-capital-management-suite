-- +goose Up
CREATE TABLE persona_icons (
    tenant_id uuid NOT NULL REFERENCES tenant (tenant_id),
    persona_id text NOT NULL CHECK (btrim(persona_id) <> ''),
    icon jsonb NOT NULL CHECK (jsonb_typeof(icon) = 'object'),
    initial_icon jsonb NOT NULL CHECK (jsonb_typeof(initial_icon) = 'object'),
    revision bigint NOT NULL CHECK (revision > 0),
    PRIMARY KEY (tenant_id, persona_id)
);
ALTER TABLE persona_icons ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_icons FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_icons
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON persona_icons TO hcmnext_agent_app;

CREATE TABLE persona_icon_events (
    tenant_id uuid NOT NULL REFERENCES tenant (tenant_id),
    persona_id text NOT NULL,
    revision bigint NOT NULL CHECK (revision > 0),
    action text NOT NULL CHECK (action IN ('create', 'backfill', 'regenerate', 'shuffle', 'reset', 'undo')),
    actor_id text NOT NULL CHECK (btrim(actor_id) <> ''),
    occurred_at timestamptz NOT NULL,
    previous_icon jsonb,
    icon jsonb NOT NULL,
    PRIMARY KEY (tenant_id, persona_id, revision),
    FOREIGN KEY (tenant_id, persona_id) REFERENCES persona_icons (tenant_id, persona_id)
);
ALTER TABLE persona_icon_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_icon_events FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_icon_events
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE TRIGGER persona_icon_events_immutable
    BEFORE UPDATE OR DELETE ON persona_icon_events
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
GRANT SELECT, INSERT ON persona_icon_events TO hcmnext_agent_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM persona_icons) THEN
        RAISE EXCEPTION 'cannot remove retained agent icons';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE persona_icon_events;
DROP TABLE persona_icons;
