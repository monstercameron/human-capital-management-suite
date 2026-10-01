-- AGENTP-006 serializes reviewer grant and owner revocation with publication.
-- The application resolver remains SELECT-only and holds the same advisory
-- keys through its caller's publish transaction.
-- +goose Up

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION persona_review_lock_owner_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended('persona-review-owner:' || OLD.tenant_id::text || ':' || OLD.persona_id, 0));
    RETURN NEW;
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION persona_review_lock_owner_change() FROM PUBLIC;
DROP TRIGGER IF EXISTS persona_review_owner_change_lock ON persona_owners;
CREATE TRIGGER persona_review_owner_change_lock
    BEFORE UPDATE ON persona_owners
    FOR EACH ROW EXECUTE FUNCTION persona_review_lock_owner_change();

-- Grant and decision revocation use a shared key based on the immutable grant
-- id. A publish that has resolved either row therefore serializes with both
-- kinds of revocation without requiring UPDATE on the app role.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION persona_review_lock_authority_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended('persona-review-grant:' || OLD.tenant_id::text || ':' || OLD.grant_id, 0));
    RETURN NEW;
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION persona_review_lock_authority_change() FROM PUBLIC;
DROP TRIGGER IF EXISTS persona_review_00_lock_authority_change ON persona_review_decision;
CREATE TRIGGER persona_review_00_lock_authority_change
    BEFORE UPDATE ON persona_review_decision
    FOR EACH ROW EXECUTE FUNCTION persona_review_lock_authority_change();
DROP TRIGGER IF EXISTS persona_review_00_lock_grant_change ON persona_review_grant;
CREATE TRIGGER persona_review_00_lock_grant_change
    BEFORE UPDATE ON persona_review_grant
    FOR EACH ROW EXECUTE FUNCTION persona_review_lock_authority_change();

-- +goose Down
DROP TRIGGER persona_review_00_lock_authority_change ON persona_review_decision;
DROP TRIGGER persona_review_00_lock_grant_change ON persona_review_grant;
DROP FUNCTION persona_review_lock_authority_change();
DROP TRIGGER persona_review_owner_change_lock ON persona_owners;
DROP FUNCTION persona_review_lock_owner_change();
