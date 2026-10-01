-- AGENTP-006 durable evidence pins for a persona publication event.
-- Ownership contract: hcmnext_agent_app is a trusted application-service
-- credential and must never be exposed to tenant/user SQL. Its INSERT grant
-- is retained for non-publication lifecycle events. This CHECK validates the
-- shape and digest linkage of publication pins; it cannot authenticate the
-- external review/evaluation authorities. Production code must use
-- TenantStore.Publish with composition-root authorities. Until the review,
-- evaluation and live-grant authorities have DB-verifiable evidence tables or
-- signatures, arbitrary SQL running as hcmnext_agent_app is privileged and
-- can forge structurally valid pins; this schema does not claim otherwise.
-- +goose Up

ALTER TABLE persona_lifecycle_events
    ADD COLUMN profile_digest text NOT NULL DEFAULT '',
    ADD COLUMN review_digest text NOT NULL DEFAULT '',
    ADD COLUMN reviewer_id text NOT NULL DEFAULT '',
    ADD COLUMN evaluation_digest text NOT NULL DEFAULT '',
    ADD COLUMN evaluation_profile_digest text NOT NULL DEFAULT '',
    ADD COLUMN evaluation_suite_digest text NOT NULL DEFAULT '';

ALTER TABLE persona_lifecycle_events
    ADD CONSTRAINT persona_publication_requires_evidence CHECK (
        to_state <> 'PUBLISHED' OR (
            btrim(profile_digest) <> ''
            AND btrim(review_digest) <> ''
            AND btrim(reviewer_id) <> ''
            AND reviewer_id = actor_id
            AND btrim(evaluation_digest) <> ''
            AND evaluation_profile_digest = profile_digest
            AND btrim(evaluation_suite_digest) <> ''
        )
    ) NOT VALID;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM persona_lifecycle_events WHERE to_state='PUBLISHED') THEN
        RAISE EXCEPTION 'cannot remove retained persona publication evidence';
    END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE persona_lifecycle_events
    DROP CONSTRAINT persona_publication_requires_evidence,
    DROP COLUMN evaluation_suite_digest,
    DROP COLUMN evaluation_profile_digest,
    DROP COLUMN evaluation_digest,
    DROP COLUMN reviewer_id,
    DROP COLUMN review_digest,
    DROP COLUMN profile_digest;
