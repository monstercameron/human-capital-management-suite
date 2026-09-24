-- REV-102-06 adversarial follow-up: preserve legacy high-risk idempotency keys
-- and generic transaction commits, which may contain any write class.
-- The class remains attached to each row after this one-time backfill.

-- +goose Up
UPDATE idempotency_record
SET retention_class = 'PERMANENT_TOMBSTONE'
WHERE status IN ('RESERVED', 'COMPLETED')
  AND (
      capability_id = 'transaction.commit'
      OR capability_id ~* '(payroll|financial|finance|payment|government|regulatory|tax_|tax[.]|irreversible|compensation|base_pay|salary|wage|budget|rewards|benefits|workflow)'
  );

-- +goose Down
-- This is a one-way retention-policy tightening. Reverting it could make a
-- financial or irreversible idempotency key eligible for deletion.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM idempotency_record
        WHERE retention_class = 'PERMANENT_TOMBSTONE'
          AND (
              capability_id = 'transaction.commit'
              OR capability_id ~* '(payroll|financial|finance|payment|government|regulatory|tax_|tax[.]|irreversible|compensation|base_pay|salary|wage|budget|rewards|benefits|workflow)'
          )
    ) THEN
        RAISE EXCEPTION 'cannot downgrade protected idempotency retention policy';
    END IF;
END
$$;
-- +goose StatementEnd
