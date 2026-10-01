-- AGENTP-021 / AGENTP-023: trusted immutable persona model-route publisher.
-- The request-serving role remains read-only; only the dedicated publisher
-- capability may append route policies after resolving trusted qualification.
-- +goose Up

-- +goose StatementBegin
DO $$
BEGIN
    CREATE ROLE hcmnext_persona_model_route_authority
        NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
        NOLOGIN NOREPLICATION NOBYPASSRLS;
EXCEPTION
    WHEN duplicate_object OR unique_violation THEN NULL;
END
$$;
-- +goose StatementEnd

ALTER ROLE hcmnext_persona_model_route_authority
    NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOLOGIN NOREPLICATION NOBYPASSRLS;

-- +goose StatementBegin
DO $$
DECLARE target_schema text := current_schema();
BEGIN
    EXECUTE format('GRANT USAGE ON SCHEMA %I TO hcmnext_persona_model_route_authority', target_schema);
END
$$;
-- +goose StatementEnd

ALTER TABLE persona_model_route_policy
    ADD COLUMN route_payload_digest text,
    ADD COLUMN evaluation_run_id text,
    ADD COLUMN evaluation_model_digest text,
    ADD CONSTRAINT persona_model_route_payload_digest_check
        CHECK (route_payload_digest IS NULL OR route_payload_digest ~ '^sha256:[0-9a-f]{64}$'),
    ADD CONSTRAINT persona_model_route_payload_binding_check
        CHECK (route_payload_digest IS NULL OR route_payload_digest = policy_digest),
    ADD CONSTRAINT persona_model_route_evaluation_model_digest_check
        CHECK (evaluation_model_digest IS NULL OR evaluation_model_digest ~ '^sha256:[0-9a-f]{64}$'),
    ADD CONSTRAINT persona_model_route_evaluation_pair_check
        CHECK ((evaluation_run_id IS NULL AND evaluation_model_digest IS NULL) OR
               (btrim(evaluation_run_id) <> '' AND evaluation_model_digest IS NOT NULL));

GRANT SELECT ON persona_evaluation_evidence TO hcmnext_persona_model_route_authority;
GRANT SELECT, INSERT ON persona_model_route_policy TO hcmnext_persona_model_route_authority;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM persona_model_route_policy WHERE route_payload_digest IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot remove retained persona model-route publications';
    END IF;
END $$;
-- +goose StatementEnd
REVOKE ALL PRIVILEGES ON persona_evaluation_evidence, persona_model_route_policy FROM hcmnext_persona_model_route_authority;
-- +goose StatementBegin
DO $$
DECLARE target_schema text := current_schema();
BEGIN
    EXECUTE format('REVOKE USAGE ON SCHEMA %I FROM hcmnext_persona_model_route_authority', target_schema);
END
$$;
-- +goose StatementEnd
ALTER TABLE persona_model_route_policy
    DROP CONSTRAINT persona_model_route_payload_digest_check,
    DROP CONSTRAINT persona_model_route_payload_binding_check,
    DROP CONSTRAINT persona_model_route_evaluation_model_digest_check,
    DROP CONSTRAINT persona_model_route_evaluation_pair_check,
    DROP COLUMN route_payload_digest,
    DROP COLUMN evaluation_run_id,
    DROP COLUMN evaluation_model_digest;
DROP ROLE hcmnext_persona_model_route_authority;
