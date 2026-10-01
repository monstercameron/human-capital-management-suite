-- AGENTP-004 durable persona versions, lifecycle, ownership and placements.
-- This belongs to the isolated agent database, not the core migration tree.
-- +goose Up

CREATE TABLE persona_versions (
    tenant_id       uuid        NOT NULL REFERENCES tenant (tenant_id),
    persona_id      text        NOT NULL CHECK (btrim(persona_id) <> ''),
    version         bigint      NOT NULL CHECK (version > 0),
    agent_version   text        NOT NULL CHECK (btrim(agent_version) <> ''),
    handle          text        NOT NULL CHECK (btrim(handle) <> ''),
    display_name    text        NOT NULL CHECK (btrim(display_name) <> ''),
    profile         jsonb       NOT NULL CHECK (jsonb_typeof(profile) = 'object'),
    content_digest  text        NOT NULL CHECK (btrim(content_digest) <> ''),
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, persona_id, version)
);
CREATE INDEX persona_versions_persona ON persona_versions (tenant_id, persona_id, version DESC);
CREATE TRIGGER persona_versions_immutable
    BEFORE UPDATE OR DELETE ON persona_versions
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

CREATE TABLE persona_lifecycle_events (
    tenant_id       uuid        NOT NULL REFERENCES tenant (tenant_id),
    event_sequence  bigserial   NOT NULL,
    event_id        text        NOT NULL CHECK (btrim(event_id) <> ''),
    persona_id      text        NOT NULL CHECK (btrim(persona_id) <> ''),
    persona_version bigint      NOT NULL CHECK (persona_version > 0),
    from_state      text        NOT NULL CHECK (from_state IN ('', 'DRAFT', 'IN_REVIEW', 'PUBLISHED', 'SUSPENDED', 'RETIRED')),
    to_state        text        NOT NULL CHECK (to_state IN ('DRAFT', 'IN_REVIEW', 'PUBLISHED', 'SUSPENDED', 'RETIRED')),
    reason          text        NOT NULL CHECK (btrim(reason) <> ''),
    actor_id        text        NOT NULL CHECK (btrim(actor_id) <> ''),
    occurred_at     timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, event_sequence),
    UNIQUE (tenant_id, event_id)
);
CREATE INDEX persona_lifecycle_current ON persona_lifecycle_events
    (tenant_id, persona_id, persona_version, event_sequence DESC);
CREATE TRIGGER persona_lifecycle_events_immutable
    BEFORE UPDATE OR DELETE ON persona_lifecycle_events
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

CREATE TABLE persona_owners (
    tenant_id     uuid        NOT NULL REFERENCES tenant (tenant_id),
    persona_id    text        NOT NULL CHECK (btrim(persona_id) <> ''),
    owner_role    text        NOT NULL CHECK (owner_role IN ('BUSINESS_OWNER', 'TECHNICAL_STEWARD')),
    principal_id  text        NOT NULL CHECK (btrim(principal_id) <> ''),
    assigned_by   text        NOT NULL CHECK (btrim(assigned_by) <> ''),
    assigned_at   timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, persona_id, owner_role)
);

-- The current installation projection retains its persona version reference
-- without a foreign key so restore can identify and suspend a missing image.
-- Placement policy is a normalized JSON snapshot of ChannelPersonaPolicy;
-- changing the grant must advance both revision and revocation_epoch.
CREATE TABLE persona_installations (
    tenant_id          uuid        NOT NULL REFERENCES tenant (tenant_id),
    installation_id    text        NOT NULL CHECK (btrim(installation_id) <> ''),
    persona_id         text        NOT NULL CHECK (btrim(persona_id) <> ''),
    persona_version    bigint      NOT NULL CHECK (persona_version > 0),
    conversation_id    text        NOT NULL CHECK (btrim(conversation_id) <> ''),
    conversation_class text        NOT NULL CHECK (conversation_class IN ('PUBLIC', 'PRIVATE', 'GROUP_DM', 'ONE_TO_ONE', 'EXTERNAL', 'CROSS_COMPANY')),
    installer_id       text        NOT NULL CHECK (btrim(installer_id) <> ''),
    channel_policy     jsonb       NOT NULL CHECK (
        jsonb_typeof(channel_policy) = 'object'
        AND channel_policy ?& ARRAY[
            'max_tier', 'allowed_data_classes', 'always_private',
            'conversation_search_allowed', 'allowed_channel_classes',
            'allow_external_members', 'allow_cross_company_members'
        ]
        AND channel_policy->>'max_tier' IN ('T0', 'T1', 'T2', 'T3', 'T4')
        AND jsonb_typeof(channel_policy->'allowed_data_classes') = 'array'
        AND jsonb_typeof(channel_policy->'always_private') = 'boolean'
        AND jsonb_typeof(channel_policy->'conversation_search_allowed') = 'boolean'
        AND jsonb_typeof(channel_policy->'allowed_channel_classes') = 'array'
        AND jsonb_typeof(channel_policy->'allow_external_members') = 'boolean'
        AND jsonb_typeof(channel_policy->'allow_cross_company_members') = 'boolean'
    ),
    state              text        NOT NULL CHECK (state IN ('ACTIVE', 'SUSPENDED', 'RETIRED', 'KILLED')),
    suspension_reason  text        NOT NULL DEFAULT '',
    revision           bigint      NOT NULL DEFAULT 1 CHECK (revision > 0),
    revocation_epoch   bigint      NOT NULL DEFAULT 1 CHECK (revocation_epoch > 0),
    created_at         timestamptz NOT NULL,
    updated_at         timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, installation_id),
    CHECK (state <> 'SUSPENDED' OR btrim(suspension_reason) <> '')
);
CREATE INDEX persona_installations_conversation ON persona_installations
    (tenant_id, conversation_id, state, persona_id, installation_id);
CREATE INDEX persona_installations_persona ON persona_installations
    (tenant_id, persona_id, persona_version, state, installation_id);

ALTER TABLE persona_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_versions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_versions
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE persona_lifecycle_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_lifecycle_events FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_lifecycle_events
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

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

GRANT SELECT, INSERT ON persona_versions, persona_lifecycle_events TO hcmnext_agent_app;
GRANT USAGE, SELECT ON SEQUENCE persona_lifecycle_events_event_sequence_seq TO hcmnext_agent_app;
GRANT SELECT, INSERT, UPDATE ON persona_owners, persona_installations TO hcmnext_agent_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM persona_versions)
        OR EXISTS (SELECT 1 FROM persona_lifecycle_events)
        OR EXISTS (SELECT 1 FROM persona_owners)
        OR EXISTS (SELECT 1 FROM persona_installations) THEN
        RAISE EXCEPTION 'cannot remove retained persona state';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE persona_installations;
DROP TABLE persona_owners;
DROP TABLE persona_lifecycle_events;
DROP TABLE persona_versions;
