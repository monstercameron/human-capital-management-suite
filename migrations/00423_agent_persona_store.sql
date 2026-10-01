-- AGENTP-004: durable persona versions, lifecycle evidence, ownership, and
-- installation recovery state.

-- +goose Up

CREATE TABLE persona_versions (
    tenant_id          uuid        NOT NULL REFERENCES tenant(tenant_id),
    persona_id         text        NOT NULL CHECK (btrim(persona_id) <> ''),
    version            bigint      NOT NULL CHECK (version > 0),
    agent_version      text        NOT NULL CHECK (btrim(agent_version) <> ''),
    handle             text        NOT NULL CHECK (btrim(handle) <> ''),
    display_name       text        NOT NULL CHECK (btrim(display_name) <> ''),
    profile            jsonb       NOT NULL CHECK (jsonb_typeof(profile) = 'object'),
    content_digest     text        NOT NULL CHECK (btrim(content_digest) <> ''),
    created_at         timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, persona_id, version)
);

CREATE INDEX persona_versions_persona ON persona_versions (tenant_id, persona_id, version DESC);

CREATE TABLE persona_lifecycle_events (
    tenant_id          uuid        NOT NULL REFERENCES tenant(tenant_id),
    event_sequence     bigserial   NOT NULL,
    event_id           text        NOT NULL CHECK (btrim(event_id) <> ''),
    persona_id         text        NOT NULL CHECK (btrim(persona_id) <> ''),
    persona_version    bigint      NOT NULL CHECK (persona_version > 0),
    from_state         text        NOT NULL CHECK (from_state IN ('', 'DRAFT', 'IN_REVIEW', 'PUBLISHED', 'SUSPENDED', 'RETIRED')),
    to_state           text        NOT NULL CHECK (to_state IN ('DRAFT', 'IN_REVIEW', 'PUBLISHED', 'SUSPENDED', 'RETIRED')),
    reason             text        NOT NULL CHECK (btrim(reason) <> ''),
    actor_id           text        NOT NULL CHECK (btrim(actor_id) <> ''),
    occurred_at        timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, event_sequence),
    UNIQUE (tenant_id, event_id)
);

CREATE INDEX persona_lifecycle_current ON persona_lifecycle_events
    (tenant_id, persona_id, persona_version, event_sequence DESC);

CREATE TABLE persona_owners (
    tenant_id          uuid        NOT NULL REFERENCES tenant(tenant_id),
    persona_id         text        NOT NULL CHECK (btrim(persona_id) <> ''),
    owner_role         text        NOT NULL CHECK (owner_role IN ('BUSINESS_OWNER', 'TECHNICAL_STEWARD')),
    principal_id       text        NOT NULL CHECK (btrim(principal_id) <> ''),
    assigned_by        text        NOT NULL CHECK (btrim(assigned_by) <> ''),
    assigned_at        timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, persona_id, owner_role)
);

-- Installations are retained independently of the version image so a restore
-- can detect a missing version and suspend the installation with typed evidence.
CREATE TABLE persona_installations (
    tenant_id          uuid        NOT NULL REFERENCES tenant(tenant_id),
    installation_id    text        NOT NULL CHECK (btrim(installation_id) <> ''),
    persona_id         text        NOT NULL CHECK (btrim(persona_id) <> ''),
    persona_version    bigint      NOT NULL CHECK (persona_version > 0),
    conversation_id    text        NOT NULL CHECK (btrim(conversation_id) <> ''),
    installer_id       text        NOT NULL CHECK (btrim(installer_id) <> ''),
    state              text        NOT NULL CHECK (state IN ('ACTIVE', 'SUSPENDED', 'RETIRED')),
    suspension_reason  text        NOT NULL DEFAULT '',
    created_at         timestamptz NOT NULL,
    updated_at         timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, installation_id)
);

ALTER TABLE persona_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_versions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_versions
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE TRIGGER persona_versions_immutable
    BEFORE UPDATE OR DELETE ON persona_versions
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE persona_lifecycle_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_lifecycle_events FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_lifecycle_events
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE TRIGGER persona_lifecycle_events_immutable
    BEFORE UPDATE OR DELETE ON persona_lifecycle_events
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE persona_owners ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_owners FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_owners
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE persona_installations ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_installations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_installations
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON persona_versions, persona_lifecycle_events TO hcmnext_app;
GRANT USAGE, SELECT ON SEQUENCE persona_lifecycle_events_event_sequence_seq TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON persona_owners, persona_installations TO hcmnext_app;

-- +goose Down

DROP TABLE persona_installations;
DROP TABLE persona_owners;
DROP TABLE persona_lifecycle_events;
DROP TABLE persona_versions;
