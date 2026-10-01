-- AGENTP-021 durable, signed persona evaluation evidence.
-- Only records signed by the configured evaluation authority are accepted by
-- the application resolver. The agent application role is trusted and must
-- never be exposed to tenant/user SQL.
-- +goose Up

CREATE TABLE persona_evaluation_evidence (
    tenant_id          uuid        NOT NULL REFERENCES tenant (tenant_id),
    run_id             text        NOT NULL CHECK (btrim(run_id) <> ''),
    persona_id         text        NOT NULL CHECK (btrim(persona_id) <> ''),
    persona_version    bigint      NOT NULL CHECK (persona_version > 0),
    profile_digest     text        NOT NULL CHECK (btrim(profile_digest) <> ''),
    suite_digest       text        NOT NULL CHECK (btrim(suite_digest) <> ''),
    run_digest         text        NOT NULL CHECK (btrim(run_digest) <> ''),
    model_digest       text        NOT NULL CHECK (btrim(model_digest) <> ''),
    passed             boolean     NOT NULL,
    issued_at          timestamptz NOT NULL,
    expires_at         timestamptz NOT NULL CHECK (expires_at > issued_at),
    seal_key_id        text        NOT NULL CHECK (btrim(seal_key_id) <> ''),
    seal               bytea       NOT NULL CHECK (octet_length(seal) = 64),
    PRIMARY KEY (tenant_id, run_id),
    CHECK (profile_digest ~ '^sha256:[0-9a-f]{64}$'),
    CHECK (suite_digest ~ '^sha256:[0-9a-f]{64}$'),
    CHECK (run_digest ~ '^sha256:[0-9a-f]{64}$'),
    CHECK (model_digest ~ '^sha256:[0-9a-f]{64}$')
);
CREATE INDEX persona_evaluation_profile ON persona_evaluation_evidence
    (tenant_id, persona_id, persona_version, profile_digest, expires_at DESC);
CREATE TRIGGER persona_evaluation_evidence_immutable
    BEFORE UPDATE OR DELETE ON persona_evaluation_evidence
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
ALTER TABLE persona_evaluation_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_evaluation_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_evaluation_evidence
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON persona_evaluation_evidence TO hcmnext_agent_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM persona_evaluation_evidence) THEN
        RAISE EXCEPTION 'cannot remove retained persona evaluation evidence';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE persona_evaluation_evidence;
