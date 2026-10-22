-- AGENTP-015: a mention refused by a mention ceiling is a recordable outcome of
-- its own. The card tells the person when they can ask again, so the outcome
-- carries that time (retry_at) and, for the hourly per-person ceiling, how many
-- times they asked (mentions_asked). The outcome is not retryable: the same
-- question would be refused the same way until retry_at.
-- +goose Up
ALTER TABLE persona_invocation_failure DROP CONSTRAINT persona_invocation_failure_failure_code_check;
ALTER TABLE persona_invocation_failure ADD COLUMN retry_at timestamptz;
ALTER TABLE persona_invocation_failure ADD COLUMN mentions_asked integer NOT NULL DEFAULT 0 CHECK (mentions_asked >= 0);
ALTER TABLE persona_invocation_failure ADD CONSTRAINT persona_invocation_failure_failure_code_check
    CHECK (failure_code IN ('MODEL_UNAVAILABLE','OUTPUT_REJECTED','DELIVERY_FAILED','ADMISSION_REFUSED','ADMISSION_UNAVAILABLE','EXECUTION_UNAVAILABLE','INVOCATION_FAILED','ANSWER_INTERRUPTED','DAILY_LIMIT_REACHED','MENTION_LIMIT_REACHED'));
-- Only a mention-limit outcome has a retry time, and it always has one.
ALTER TABLE persona_invocation_failure ADD CONSTRAINT persona_invocation_failure_retry_at_check
    CHECK ((failure_code = 'MENTION_LIMIT_REACHED') = (retry_at IS NOT NULL));

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM persona_invocation_failure WHERE failure_code = 'MENTION_LIMIT_REACHED') THEN
        RAISE EXCEPTION 'cannot remove retained persona invocation failures';
    END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE persona_invocation_failure DROP CONSTRAINT persona_invocation_failure_retry_at_check;
ALTER TABLE persona_invocation_failure DROP CONSTRAINT persona_invocation_failure_failure_code_check;
ALTER TABLE persona_invocation_failure DROP COLUMN mentions_asked;
ALTER TABLE persona_invocation_failure DROP COLUMN retry_at;
ALTER TABLE persona_invocation_failure ADD CONSTRAINT persona_invocation_failure_failure_code_check
    CHECK (failure_code IN ('MODEL_UNAVAILABLE','OUTPUT_REJECTED','DELIVERY_FAILED','ADMISSION_REFUSED','ADMISSION_UNAVAILABLE','EXECUTION_UNAVAILABLE','INVOCATION_FAILED','ANSWER_INTERRUPTED','DAILY_LIMIT_REACHED'));
