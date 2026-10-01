-- Semantic policy references are independent of manifest-bound route payloads.
-- +goose Up
ALTER TABLE persona_model_route_policy
    DROP CONSTRAINT persona_model_route_payload_binding_check,
    ADD COLUMN policy_payload jsonb,
    ADD CONSTRAINT persona_model_policy_payload_object_check
        CHECK (policy_payload IS NULL OR jsonb_typeof(policy_payload) = 'object');

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM persona_model_route_policy WHERE policy_payload IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot remove retained persona semantic model policies';
    END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE persona_model_route_policy
    DROP CONSTRAINT persona_model_policy_payload_object_check,
    DROP COLUMN policy_payload,
    ADD CONSTRAINT persona_model_route_payload_binding_check
        CHECK (route_payload_digest IS NULL OR route_payload_digest = policy_digest);
