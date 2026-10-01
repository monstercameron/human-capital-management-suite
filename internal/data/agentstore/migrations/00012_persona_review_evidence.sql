-- Durable, tenant-scoped review decisions and current persona-review grants.
-- Only the trusted review/grant authority writes these records. The agent
-- application may resolve them, but cannot manufacture or revoke authority.
-- +goose Up

CREATE TABLE persona_review_grant (
    tenant_id       uuid        NOT NULL REFERENCES tenant (tenant_id),
    grant_id        text        NOT NULL CHECK (btrim(grant_id) <> ''),
    principal_id    text        NOT NULL CHECK (btrim(principal_id) <> ''),
    permission      text        NOT NULL CHECK (permission = 'persona:review'),
    granted_at      timestamptz NOT NULL,
    expires_at      timestamptz NOT NULL,
    revoked_at      timestamptz,
    revoked_by      text        NOT NULL DEFAULT '',
    PRIMARY KEY (tenant_id, grant_id),
    CHECK (expires_at > granted_at),
    CHECK ((revoked_at IS NULL) = (revoked_by = ''))
);
CREATE INDEX persona_review_grant_current ON persona_review_grant
    (tenant_id, principal_id, permission, expires_at) WHERE revoked_at IS NULL;

CREATE TABLE persona_review_decision (
    tenant_id       uuid        NOT NULL REFERENCES tenant (tenant_id),
    review_id       text        NOT NULL CHECK (btrim(review_id) <> ''),
    persona_id      text        NOT NULL CHECK (btrim(persona_id) <> ''),
    persona_version bigint      NOT NULL CHECK (persona_version > 0),
    profile_digest  text        NOT NULL CHECK (btrim(profile_digest) <> ''),
    author_id       text        NOT NULL CHECK (btrim(author_id) <> ''),
    reviewer_id     text        NOT NULL CHECK (btrim(reviewer_id) <> '' AND reviewer_id <> author_id),
    grant_id        text        NOT NULL CHECK (btrim(grant_id) <> ''),
    permission      text        NOT NULL CHECK (permission = 'persona:review'),
    decision        text        NOT NULL CHECK (decision IN ('APPROVE', 'REJECT')),
    review_digest   text        NOT NULL CHECK (btrim(review_digest) <> ''),
    reviewed_at     timestamptz NOT NULL,
    expires_at      timestamptz NOT NULL,
    revoked_at      timestamptz,
    revoked_by      text        NOT NULL DEFAULT '',
    PRIMARY KEY (tenant_id, review_id),
    FOREIGN KEY (tenant_id, grant_id) REFERENCES persona_review_grant (tenant_id, grant_id),
    CHECK (expires_at > reviewed_at),
    CHECK ((revoked_at IS NULL) = (revoked_by = ''))
);
CREATE INDEX persona_review_decision_target ON persona_review_decision
    (tenant_id, persona_id, persona_version, profile_digest, review_id);

-- A decision/grant may be revoked once, but its authority-bearing fields and
-- original revocation fact cannot be rewritten or erased.
-- +goose StatementBegin
CREATE FUNCTION persona_review_revoke_only() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE'
       AND OLD.revoked_at IS NULL
       AND NEW.revoked_at IS NOT NULL
       AND NEW.revoked_by <> ''
       AND (to_jsonb(NEW) - 'revoked_at' - 'revoked_by') = (to_jsonb(OLD) - 'revoked_at' - 'revoked_by') THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'persona review authority records may only be revoked once';
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION persona_review_revoke_only() FROM PUBLIC;

CREATE TRIGGER persona_review_decision_revoke_only
    BEFORE UPDATE OR DELETE ON persona_review_decision
    FOR EACH ROW EXECUTE FUNCTION persona_review_revoke_only();
CREATE TRIGGER persona_review_grant_revoke_only
    BEFORE UPDATE OR DELETE ON persona_review_grant
    FOR EACH ROW EXECUTE FUNCTION persona_review_revoke_only();

ALTER TABLE persona_review_grant ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_review_grant FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_review_grant
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE persona_review_decision ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_review_decision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_review_decision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- hcmnext_agent_app is a resolver only. A separately credentialed trusted
-- review authority owns INSERT and revocation; tenant SQL cannot assert facts.
GRANT SELECT ON persona_review_grant, persona_review_decision TO hcmnext_agent_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM persona_review_decision) OR EXISTS (SELECT 1 FROM persona_review_grant) THEN
        RAISE EXCEPTION 'cannot remove retained persona review evidence';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE persona_review_decision;
DROP TABLE persona_review_grant;
DROP FUNCTION persona_review_revoke_only();
