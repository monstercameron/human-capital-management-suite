-- AGENTP-008/019 canonical chat Agent ID to persona identity registration.
-- The mapping is tenant-local and the identifiers remain reserved after revoke.
-- +goose Up

CREATE TABLE persona_chat_identities (
    tenant_id    uuid        NOT NULL REFERENCES tenant(tenant_id),
    agent_id     text        NOT NULL CHECK (btrim(agent_id) <> ''),
    persona_id   text        NOT NULL CHECK (btrim(persona_id) <> ''),
    state        text        NOT NULL DEFAULT 'ACTIVE' CHECK (state IN ('ACTIVE', 'REVOKED')),
    registered_at timestamptz NOT NULL,
    revoked_at   timestamptz,
    PRIMARY KEY (tenant_id, agent_id),
    UNIQUE (tenant_id, persona_id),
    CHECK ((state = 'ACTIVE' AND revoked_at IS NULL) OR
           (state = 'REVOKED' AND revoked_at IS NOT NULL))
);
CREATE INDEX persona_chat_identities_persona
    ON persona_chat_identities (tenant_id, persona_id, state);

-- Identity columns and the revoke timestamp are immutable once written. A
-- registration can be revoked exactly once; a revoked identity cannot be
-- reactivated or rebound to another persona.
-- +goose StatementBegin
CREATE FUNCTION persona_chat_identity_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.tenant_id <> OLD.tenant_id
       OR NEW.agent_id <> OLD.agent_id
       OR NEW.persona_id <> OLD.persona_id
       OR NEW.registered_at <> OLD.registered_at THEN
        RAISE EXCEPTION 'persona chat identity binding is immutable';
    END IF;
    IF OLD.state = 'REVOKED' THEN
        RAISE EXCEPTION 'persona chat identity is already revoked';
    END IF;
    IF NEW.state <> 'REVOKED' OR NEW.revoked_at IS NULL THEN
        RAISE EXCEPTION 'persona chat identity may only transition to REVOKED';
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER persona_chat_identity_guard
    BEFORE UPDATE ON persona_chat_identities
    FOR EACH ROW EXECUTE FUNCTION persona_chat_identity_guard();

ALTER TABLE persona_chat_identities ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_chat_identities FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_chat_identities
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE ON persona_chat_identities TO hcmnext_agent_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM persona_chat_identities) THEN
        RAISE EXCEPTION 'cannot remove retained persona chat identities';
    END IF;
END $$;
-- +goose StatementEnd
DROP TRIGGER persona_chat_identity_guard ON persona_chat_identities;
DROP FUNCTION persona_chat_identity_guard();
DROP TABLE persona_chat_identities;
