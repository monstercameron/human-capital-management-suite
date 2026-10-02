-- AGENTRUN recovery: an answer interrupted by a dying worker is a recordable,
-- retryable outcome. A claim that never reached a run, and a run whose worker is
-- gone and which may already have spent a model call, are finished by the
-- recovery sweep with this code instead of waiting forever.
-- +goose Up
ALTER TABLE persona_invocation_failure DROP CONSTRAINT persona_invocation_failure_failure_code_check;
ALTER TABLE persona_invocation_failure DROP CONSTRAINT persona_invocation_failure_check;
ALTER TABLE persona_invocation_failure ADD CONSTRAINT persona_invocation_failure_failure_code_check
    CHECK (failure_code IN ('MODEL_UNAVAILABLE','OUTPUT_REJECTED','DELIVERY_FAILED','ADMISSION_REFUSED','ADMISSION_UNAVAILABLE','EXECUTION_UNAVAILABLE','INVOCATION_FAILED','ANSWER_INTERRUPTED'));
ALTER TABLE persona_invocation_failure ADD CONSTRAINT persona_invocation_failure_check
    CHECK (NOT retryable OR failure_code IN ('MODEL_UNAVAILABLE','ADMISSION_UNAVAILABLE','EXECUTION_UNAVAILABLE','ANSWER_INTERRUPTED'));

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM persona_invocation_failure WHERE failure_code = 'ANSWER_INTERRUPTED') THEN
        RAISE EXCEPTION 'cannot remove retained persona invocation failures';
    END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE persona_invocation_failure DROP CONSTRAINT persona_invocation_failure_check;
ALTER TABLE persona_invocation_failure DROP CONSTRAINT persona_invocation_failure_failure_code_check;
ALTER TABLE persona_invocation_failure ADD CONSTRAINT persona_invocation_failure_failure_code_check
    CHECK (failure_code IN ('MODEL_UNAVAILABLE','OUTPUT_REJECTED','DELIVERY_FAILED','ADMISSION_REFUSED','ADMISSION_UNAVAILABLE','EXECUTION_UNAVAILABLE','INVOCATION_FAILED'));
ALTER TABLE persona_invocation_failure ADD CONSTRAINT persona_invocation_failure_check
    CHECK (NOT retryable OR failure_code IN ('MODEL_UNAVAILABLE','ADMISSION_UNAVAILABLE','EXECUTION_UNAVAILABLE'));
