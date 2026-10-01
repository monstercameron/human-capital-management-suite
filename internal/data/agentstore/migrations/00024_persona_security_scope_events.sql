-- Preserve revocation and reviewed version-reactivation evidence across restart.
-- +goose Up
CREATE TABLE persona_security_scope_event (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    event_id text NOT NULL CHECK (btrim(event_id) <> ''),
    scope_kind text NOT NULL CHECK (scope_kind IN ('TENANT','PRINCIPAL','PERSONA','VERSION','INSTALLATION','RUN')),
    scope_key text NOT NULL CHECK (btrim(scope_key) <> ''),
    epoch bigint NOT NULL CHECK (epoch > 0),
    state text NOT NULL CHECK (state IN ('ACTIVE','REVOKED')),
    reason text NOT NULL CHECK (btrim(reason) <> ''),
    occurred_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id,event_id),
    UNIQUE (tenant_id,scope_kind,scope_key,epoch),
    FOREIGN KEY (tenant_id,scope_kind,scope_key) REFERENCES persona_security_scope(tenant_id,scope_kind,scope_key)
);
CREATE TRIGGER persona_security_scope_event_immutable BEFORE UPDATE OR DELETE ON persona_security_scope_event
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
ALTER TABLE persona_security_scope_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_security_scope_event FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_security_scope_event
    USING (tenant_id = NULLIF(current_setting('app.tenant_id',true),'')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id',true),'')::uuid);
GRANT SELECT, INSERT ON persona_security_scope_event TO hcmnext_agent_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM persona_security_scope_event) THEN
        RAISE EXCEPTION 'cannot remove retained persona security scope evidence';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE persona_security_scope_event;
