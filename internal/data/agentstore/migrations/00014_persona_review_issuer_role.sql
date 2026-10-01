-- AGENTP-006 trusted review evidence writer. Tenant/user-serving agent SQL
-- remains read-only for these facts. Deployment creates a separately
-- credentialed login and grants it membership in this non-login role only for
-- the review authority service; credentials never belong to tenant SQL.
-- +goose Up

-- +goose StatementBegin
DO $$
BEGIN
    CREATE ROLE hcmnext_persona_review_authority
        NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
        NOLOGIN NOREPLICATION NOBYPASSRLS;
EXCEPTION
    WHEN duplicate_object OR unique_violation THEN NULL;
END
$$;
-- +goose StatementEnd

ALTER ROLE hcmnext_persona_review_authority
    NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOLOGIN NOREPLICATION NOBYPASSRLS;

-- The review authority reads the exact immutable persona and current owner,
-- issues grants and decisions, and may only update the revocation columns.
-- It has no ability to rewrite the authority-bearing content of prior records.
-- +goose StatementBegin
DO $$
DECLARE target_schema text := current_schema();
BEGIN
    EXECUTE format('GRANT USAGE ON SCHEMA %I TO hcmnext_persona_review_authority', target_schema);
END
$$;
-- +goose StatementEnd

GRANT SELECT ON persona_versions, persona_owners, persona_lifecycle_events TO hcmnext_persona_review_authority;
GRANT SELECT, INSERT ON persona_review_grant, persona_review_decision TO hcmnext_persona_review_authority;
GRANT UPDATE (revoked_at, revoked_by) ON persona_review_grant, persona_review_decision TO hcmnext_persona_review_authority;

-- +goose Down
REVOKE ALL PRIVILEGES ON persona_versions, persona_owners, persona_lifecycle_events,
    persona_review_grant, persona_review_decision FROM hcmnext_persona_review_authority;
DROP ROLE hcmnext_persona_review_authority;
