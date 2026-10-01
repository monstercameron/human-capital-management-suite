-- Owner: promotion domain lane. Phase: P1B.
-- storage-disposition: people_promotion_write_evidence | additive provenance | local PostgreSQL | Promotion local ACID commit | tenant-scoped.

-- +goose Up

ALTER TABLE people_promotion_write_evidence
    ADD COLUMN intent_id uuid,
    ADD COLUMN proposal_revision_number bigint,
    ADD COLUMN assignment_row_id uuid,
    ADD COLUMN assignment_digest text;

ALTER TABLE people_promotion_write_evidence
    ADD CONSTRAINT people_promotion_write_provenance_all_or_none CHECK (
        (intent_id IS NULL AND proposal_revision_number IS NULL AND assignment_row_id IS NULL AND assignment_digest IS NULL)
        OR (intent_id IS NOT NULL AND proposal_revision_number IS NOT NULL AND proposal_revision_number > 0 AND assignment_row_id IS NOT NULL
            AND assignment_digest IS NOT NULL AND assignment_digest ~ '^[0-9a-f]{64}$'
            AND length(proposal_digest) > 0 AND length(actor_principal_id) > 0
            AND length(expected_revision) > 0 AND length(authority_decision) > 0)
    );

CREATE UNIQUE INDEX people_promotion_write_proof_unique
    ON people_promotion_write_evidence (tenant_id, intent_id, proposal_revision_id, assignment_id, field_path)
    WHERE intent_id IS NOT NULL;

-- +goose Down

DROP INDEX people_promotion_write_proof_unique;
ALTER TABLE people_promotion_write_evidence
    DROP CONSTRAINT people_promotion_write_provenance_all_or_none,
    DROP COLUMN assignment_digest,
    DROP COLUMN assignment_row_id,
    DROP COLUMN proposal_revision_number,
    DROP COLUMN intent_id;
