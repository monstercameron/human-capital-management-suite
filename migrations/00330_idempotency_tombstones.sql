-- REV-102-06: retain compact uniqueness records for capabilities whose
-- financial, government or irreversible effects cannot safely be repeated.
-- Existing rows remain expiring by default; callers declare permanence on
-- new reservations through RetentionPolicy.Class.

-- +goose Up
ALTER TABLE idempotency_record
    ALTER COLUMN expires_at DROP NOT NULL;

ALTER TABLE idempotency_record
    ADD COLUMN retention_class text NOT NULL DEFAULT 'EXPIRING';

DROP INDEX idempotency_record_expiry;
CREATE INDEX idempotency_record_expiry
    ON idempotency_record (tenant_id, expires_at)
    WHERE expires_at IS NOT NULL;

ALTER TABLE idempotency_record
    DROP CONSTRAINT idempotency_record_status_allowed,
    ADD CONSTRAINT idempotency_record_status_allowed CHECK (
        status IN ('RESERVED', 'COMPLETED', 'TOMBSTONE')
    );

ALTER TABLE idempotency_record
    DROP CONSTRAINT idempotency_record_expires_after_created,
    ADD CONSTRAINT idempotency_record_expires_after_created CHECK (
        (status = 'TOMBSTONE' AND expires_at IS NULL)
        OR (status <> 'TOMBSTONE' AND expires_at > created_at)
    ),
    ADD CONSTRAINT idempotency_record_retention_class_allowed CHECK (
        retention_class IN ('EXPIRING', 'PERMANENT_TOMBSTONE')
    ),
    ADD CONSTRAINT idempotency_record_tombstone_is_compact CHECK (
        status <> 'TOMBSTONE' OR (
            retention_class = 'PERMANENT_TOMBSTONE'
            AND result_ref IS NULL AND event_ref IS NULL
            AND effect_identity IS NULL AND evidence_id IS NULL
        )
    );

-- +goose Down
-- Compacted identities cannot be reconstructed. Refuse downgrade while any
-- tombstone exists instead of silently discarding permanent key protection.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM idempotency_record WHERE status = 'TOMBSTONE') THEN
        RAISE EXCEPTION 'cannot remove idempotency tombstones during downgrade';
    END IF;
END
$$;
-- +goose StatementEnd

ALTER TABLE idempotency_record
    DROP CONSTRAINT idempotency_record_tombstone_is_compact,
    DROP CONSTRAINT idempotency_record_retention_class_allowed,
    DROP CONSTRAINT idempotency_record_expires_after_created,
    ADD CONSTRAINT idempotency_record_expires_after_created CHECK (
        expires_at > created_at
    ),
    DROP CONSTRAINT idempotency_record_status_allowed,
    ADD CONSTRAINT idempotency_record_status_allowed CHECK (
        status IN ('RESERVED', 'COMPLETED')
    ),
    DROP COLUMN retention_class,
    ALTER COLUMN expires_at SET NOT NULL;

DROP INDEX idempotency_record_expiry;
CREATE INDEX idempotency_record_expiry
    ON idempotency_record (tenant_id, expires_at);
