-- REV-102-03: mutable compensation recovery state must retain a complete,
-- immutable record of every state and payload revision.

-- +goose Up

ALTER TABLE workflow_compensation_operation
    ADD COLUMN operation_version bigint NOT NULL DEFAULT 1,
    ADD CONSTRAINT workflow_compensation_operation_version_positive
        CHECK (operation_version >= 1);

CREATE TABLE workflow_compensation_operation_history (
    tenant_id       uuid NOT NULL REFERENCES tenant (tenant_id),
    history_id      uuid NOT NULL DEFAULT gen_random_uuid(),
    capability_id   semantic_key NOT NULL,
    effect_ref      semantic_key NOT NULL,
    idempotency_key semantic_key NOT NULL,
    operation_version bigint NOT NULL CHECK (operation_version >= 1),
    change_kind     text NOT NULL CHECK (change_kind IN ('SNAPSHOT', 'INSERT', 'UPDATE')),
    prior_state     text,
    state           text NOT NULL CHECK (state IN ('RESERVED', 'EFFECT_RECORDED', 'COMPLETED')),
    prior_payload   jsonb,
    payload         jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    recorded_at     timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (tenant_id, history_id),
    UNIQUE (tenant_id, capability_id, effect_ref, idempotency_key, operation_version),
    CONSTRAINT workflow_compensation_operation_history_prior_pair CHECK (
        (prior_state IS NULL AND prior_payload IS NULL)
        OR (prior_state IN ('RESERVED', 'EFFECT_RECORDED', 'COMPLETED')
            AND prior_payload IS NOT NULL
            AND jsonb_typeof(prior_payload) = 'object')
    ),
    CONSTRAINT workflow_compensation_operation_history_first_revision CHECK (
        (change_kind IN ('SNAPSHOT', 'INSERT') AND operation_version = 1
            AND prior_state IS NULL AND prior_payload IS NULL)
        OR (change_kind = 'UPDATE' AND operation_version > 1
            AND prior_state IS NOT NULL AND prior_payload IS NOT NULL)
    )
);

ALTER TABLE workflow_compensation_operation_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_compensation_operation_history FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workflow_compensation_operation_history
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

CREATE TRIGGER workflow_compensation_operation_history_forbid_mutation
    BEFORE UPDATE OR DELETE ON workflow_compensation_operation_history
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON workflow_compensation_operation_history FROM PUBLIC;
REVOKE UPDATE, DELETE ON workflow_compensation_operation_history FROM hcmnext_app;
GRANT SELECT, INSERT ON workflow_compensation_operation_history TO hcmnext_app;

-- Record one reconstruction snapshot for every reservation that predates
-- this migration. Later mutations append a new version in the same transaction.
INSERT INTO workflow_compensation_operation_history (
    tenant_id, capability_id, effect_ref, idempotency_key, operation_version,
    change_kind, state, payload, recorded_at
)
SELECT tenant_id, capability_id, effect_ref, idempotency_key, operation_version,
       'SNAPSHOT', state, payload, updated_at
FROM workflow_compensation_operation;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION workflow_compensation_operation_capture_history()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    allowed_transition boolean;
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'workflow compensation operation reservations cannot be deleted';
    END IF;

    IF NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
       OR NEW.capability_id IS DISTINCT FROM OLD.capability_id
       OR NEW.effect_ref IS DISTINCT FROM OLD.effect_ref
       OR NEW.idempotency_key IS DISTINCT FROM OLD.idempotency_key
       OR NEW.request_digest IS DISTINCT FROM OLD.request_digest THEN
        RAISE EXCEPTION 'workflow compensation operation identity is immutable';
    END IF;

    allowed_transition :=
        (OLD.state = 'RESERVED' AND NEW.state IN ('RESERVED', 'EFFECT_RECORDED', 'COMPLETED'))
        OR (OLD.state = 'EFFECT_RECORDED' AND NEW.state IN ('EFFECT_RECORDED', 'COMPLETED'));
    IF NOT allowed_transition THEN
        RAISE EXCEPTION 'invalid workflow compensation operation transition: % -> %', OLD.state, NEW.state;
    END IF;

    NEW.operation_version := OLD.operation_version + 1;
    INSERT INTO workflow_compensation_operation_history (
        tenant_id, capability_id, effect_ref, idempotency_key, operation_version,
        change_kind, prior_state, state, prior_payload, payload
    ) VALUES (
        OLD.tenant_id, OLD.capability_id, OLD.effect_ref, OLD.idempotency_key,
        NEW.operation_version, 'UPDATE', OLD.state, NEW.state, OLD.payload, NEW.payload
    );
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION workflow_compensation_operation_insert_history()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.state <> 'RESERVED' OR NEW.operation_version <> 1 THEN
        RAISE EXCEPTION 'workflow compensation operations must begin at version 1 in RESERVED state';
    END IF;
    INSERT INTO workflow_compensation_operation_history (
        tenant_id, capability_id, effect_ref, idempotency_key, operation_version,
        change_kind, state, payload
    ) VALUES (
        NEW.tenant_id, NEW.capability_id, NEW.effect_ref, NEW.idempotency_key,
        NEW.operation_version, 'INSERT', NEW.state, NEW.payload
    );
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER workflow_compensation_operation_capture_history
    BEFORE UPDATE OR DELETE ON workflow_compensation_operation
    FOR EACH ROW EXECUTE FUNCTION workflow_compensation_operation_capture_history();
CREATE TRIGGER workflow_compensation_operation_insert_history
    AFTER INSERT ON workflow_compensation_operation
    FOR EACH ROW EXECUTE FUNCTION workflow_compensation_operation_insert_history();

-- +goose Down
-- Preserve the append-only operation history once any row has been recorded.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM workflow_compensation_operation_history) THEN
        RAISE EXCEPTION 'cannot remove workflow compensation operation history';
    END IF;
END
$$;
-- +goose StatementEnd

DROP TRIGGER workflow_compensation_operation_insert_history ON workflow_compensation_operation;
DROP TRIGGER workflow_compensation_operation_capture_history ON workflow_compensation_operation;
DROP FUNCTION workflow_compensation_operation_insert_history();
DROP FUNCTION workflow_compensation_operation_capture_history();
DROP TABLE workflow_compensation_operation_history;
ALTER TABLE workflow_compensation_operation
    DROP CONSTRAINT workflow_compensation_operation_version_positive,
    DROP COLUMN operation_version;
