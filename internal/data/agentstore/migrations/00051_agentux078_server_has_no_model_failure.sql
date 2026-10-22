-- AGENTUX-078: a question asked of an agent on a server with no model is
-- recorded as SERVER_HAS_NO_MODEL so the card can say why nothing was answered.
-- The run path wrote that code but the failure-code check did not list it, so
-- the row was refused and the card said nothing. The outcome is not retryable:
-- asking again changes nothing until an administrator connects a model, which is
-- why the retryable check below is left as it was.
-- +goose Up
ALTER TABLE persona_invocation_failure DROP CONSTRAINT persona_invocation_failure_failure_code_check;
ALTER TABLE persona_invocation_failure ADD CONSTRAINT persona_invocation_failure_failure_code_check
    CHECK (failure_code IN ('MODEL_UNAVAILABLE','OUTPUT_REJECTED','DELIVERY_FAILED','ADMISSION_REFUSED','ADMISSION_UNAVAILABLE','EXECUTION_UNAVAILABLE','INVOCATION_FAILED','ANSWER_INTERRUPTED','DAILY_LIMIT_REACHED','MENTION_LIMIT_REACHED','SERVER_HAS_NO_MODEL'));

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM persona_invocation_failure WHERE failure_code = 'SERVER_HAS_NO_MODEL') THEN
        RAISE EXCEPTION 'cannot remove retained persona invocation failures';
    END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE persona_invocation_failure DROP CONSTRAINT persona_invocation_failure_failure_code_check;
ALTER TABLE persona_invocation_failure ADD CONSTRAINT persona_invocation_failure_failure_code_check
    CHECK (failure_code IN ('MODEL_UNAVAILABLE','OUTPUT_REJECTED','DELIVERY_FAILED','ADMISSION_REFUSED','ADMISSION_UNAVAILABLE','EXECUTION_UNAVAILABLE','INVOCATION_FAILED','ANSWER_INTERRUPTED','DAILY_LIMIT_REACHED','MENTION_LIMIT_REACHED'));
