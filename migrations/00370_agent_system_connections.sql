-- AGENT2-007: durable agent-facing system connection revisions, per-user
-- external account links (credential references only, never secrets) and
-- connection revocation state with epochs.

-- +goose Up

CREATE TABLE agent_connection_revision (
    tenant_id            uuid        NOT NULL REFERENCES tenant(tenant_id),
    connection_id        text        NOT NULL CHECK (btrim(connection_id) <> ''),
    revision             bigint      NOT NULL CHECK (revision >= 1),
    endpoint             text        NOT NULL CHECK (btrim(endpoint) <> '' AND endpoint !~ '\s'),
    connector_id         text        NOT NULL CHECK (btrim(connector_id) <> ''),
    connector_version    text        NOT NULL CHECK (btrim(connector_version) <> ''),
    credential_mode      text        NOT NULL CHECK (credential_mode IN ('USER_DELEGATED', 'BROKERED')),
    brokered_credential  jsonb,
    skills               jsonb       NOT NULL CHECK (jsonb_typeof(skills) = 'array' AND jsonb_array_length(skills) > 0),
    grants               jsonb       NOT NULL CHECK (jsonb_typeof(grants) = 'array' AND jsonb_array_length(grants) > 0),
    approval             jsonb,
    connection_epoch     bigint      NOT NULL DEFAULT 1 CHECK (connection_epoch >= 1),
    revoked_at           timestamptz,
    revoked_by           text,
    revoked_reason       text,
    revoked_evidence_ref text,
    created_at           timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, connection_id),
    CHECK ((credential_mode = 'BROKERED') = (brokered_credential IS NOT NULL)),
    CHECK (credential_mode <> 'BROKERED' OR approval IS NOT NULL),
    CHECK ((revoked_at IS NULL) = (revoked_by IS NULL)
       AND (revoked_at IS NULL) = (revoked_reason IS NULL)
       AND (revoked_at IS NULL) = (revoked_evidence_ref IS NULL))
);

CREATE TABLE agent_connection_user_state (
    tenant_id           uuid        NOT NULL REFERENCES tenant(tenant_id),
    connection_id       text        NOT NULL CHECK (btrim(connection_id) <> ''),
    user_id             text        NOT NULL CHECK (btrim(user_id) <> ''),
    user_epoch          bigint      NOT NULL CHECK (user_epoch >= 1),
    linked              boolean     NOT NULL,
    external_account_id text,
    binding             jsonb,
    linked_at           timestamptz,
    updated_at          timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, connection_id, user_id),
    FOREIGN KEY (tenant_id, connection_id) REFERENCES agent_connection_revision (tenant_id, connection_id),
    CHECK (linked = (external_account_id IS NOT NULL AND binding IS NOT NULL AND linked_at IS NOT NULL)),
    CHECK (linked OR (external_account_id IS NULL AND binding IS NULL AND linked_at IS NULL))
);

-- +goose StatementBegin
CREATE FUNCTION agent_connection_revision_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'table % is retained; DELETE is forbidden', TG_TABLE_NAME
            USING ERRCODE = '23514';
    END IF;
    -- A revision is immutable; only the epoch and the revocation record move.
    IF (to_jsonb(NEW) - 'connection_epoch' - 'revoked_at' - 'revoked_by' - 'revoked_reason' - 'revoked_evidence_ref')
        IS DISTINCT FROM
       (to_jsonb(OLD) - 'connection_epoch' - 'revoked_at' - 'revoked_by' - 'revoked_reason' - 'revoked_evidence_ref') THEN
        RAISE EXCEPTION 'table % is immutable except for epoch and revocation', TG_TABLE_NAME
            USING ERRCODE = '23514';
    END IF;
    IF NEW.connection_epoch < OLD.connection_epoch THEN
        RAISE EXCEPTION 'connection epoch must never decrease'
            USING ERRCODE = '23514';
    END IF;
    IF OLD.revoked_at IS NOT NULL AND (NEW.revoked_at IS DISTINCT FROM OLD.revoked_at
        OR NEW.revoked_by IS DISTINCT FROM OLD.revoked_by
        OR NEW.revoked_reason IS DISTINCT FROM OLD.revoked_reason
        OR NEW.revoked_evidence_ref IS DISTINCT FROM OLD.revoked_evidence_ref) THEN
        RAISE EXCEPTION 'a revoked connection cannot be reinstated or its revocation rewritten'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION agent_connection_user_state_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'table % keeps the user epoch; DELETE is forbidden', TG_TABLE_NAME
            USING ERRCODE = '23514';
    END IF;
    IF NEW.tenant_id <> OLD.tenant_id OR NEW.connection_id <> OLD.connection_id
       OR NEW.user_id <> OLD.user_id OR NEW.user_epoch < OLD.user_epoch THEN
        RAISE EXCEPTION 'user identity is fixed and the user epoch must never decrease'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER agent_connection_revision_guard
    BEFORE UPDATE OR DELETE ON agent_connection_revision
    FOR EACH ROW EXECUTE FUNCTION agent_connection_revision_guard();
CREATE TRIGGER agent_connection_user_state_guard
    BEFORE UPDATE OR DELETE ON agent_connection_user_state
    FOR EACH ROW EXECUTE FUNCTION agent_connection_user_state_guard();

ALTER TABLE agent_connection_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_connection_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_connection_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE agent_connection_user_state ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_connection_user_state FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_connection_user_state
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE ON agent_connection_revision, agent_connection_user_state TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM agent_connection_revision) THEN
        RAISE EXCEPTION 'cannot remove retained agent connection revisions';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE agent_connection_user_state;
DROP TABLE agent_connection_revision;
DROP FUNCTION agent_connection_user_state_guard();
DROP FUNCTION agent_connection_revision_guard();
