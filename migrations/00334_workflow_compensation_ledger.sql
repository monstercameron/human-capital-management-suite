-- WF-REV-010: compensation evidence, resumable operation state and closure of
-- the original semantic idempotency scope share the caller's transaction.

-- +goose Up
CREATE TABLE workflow_compensation_event (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    event_ref text NOT NULL,
    event_digest content_digest NOT NULL,
    payload jsonb NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, event_ref),
    CONSTRAINT workflow_compensation_event_ref_nonblank CHECK (btrim(event_ref) <> ''),
    CONSTRAINT workflow_compensation_event_payload_object CHECK (jsonb_typeof(payload) = 'object')
);
ALTER TABLE workflow_compensation_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_compensation_event FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workflow_compensation_event
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON workflow_compensation_event TO hcmnext_app;
CREATE TRIGGER workflow_compensation_event_forbid_mutation
    BEFORE UPDATE OR DELETE ON workflow_compensation_event
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

CREATE TABLE workflow_compensation_operation (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    capability_id semantic_key NOT NULL,
    effect_ref semantic_key NOT NULL,
    idempotency_key semantic_key NOT NULL,
    request_digest content_digest NOT NULL,
    state text NOT NULL,
    payload jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, capability_id, effect_ref, idempotency_key),
    CONSTRAINT workflow_compensation_operation_state CHECK (state IN ('RESERVED','EFFECT_RECORDED','COMPLETED')),
    CONSTRAINT workflow_compensation_operation_payload_object CHECK (jsonb_typeof(payload) = 'object')
);
ALTER TABLE workflow_compensation_operation ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_compensation_operation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workflow_compensation_operation
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON workflow_compensation_operation TO hcmnext_app;

ALTER TABLE idempotency_record
    ADD COLUMN compensated_by_event_ref text,
    DROP CONSTRAINT idempotency_record_expires_after_created,
    ADD CONSTRAINT idempotency_record_expires_after_created CHECK (
        (status = 'TOMBSTONE' AND expires_at IS NULL)
        OR (status = 'COMPENSATED' AND (expires_at IS NULL OR expires_at > created_at))
        OR (status NOT IN ('TOMBSTONE', 'COMPENSATED') AND expires_at > created_at)
    ),
    DROP CONSTRAINT idempotency_record_status_allowed,
    ADD CONSTRAINT idempotency_record_status_allowed CHECK (
        status IN ('RESERVED', 'COMPLETED', 'COMPENSATED', 'TOMBSTONE')
    ),
    ADD CONSTRAINT idempotency_record_compensated_ref CHECK (
        (status = 'COMPENSATED' AND compensated_by_event_ref IS NOT NULL)
        OR (status <> 'COMPENSATED' AND compensated_by_event_ref IS NULL)
    ),
    ADD CONSTRAINT idempotency_record_compensated_event_fk
        FOREIGN KEY (tenant_id, compensated_by_event_ref)
        REFERENCES workflow_compensation_event(tenant_id, event_ref);

-- +goose Down
-- Compensation evidence and closed scopes are durable business history.
-- Refuse rollback once one has been recorded.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM workflow_compensation_event)
       OR EXISTS (SELECT 1 FROM idempotency_record WHERE status = 'COMPENSATED') THEN
        RAISE EXCEPTION 'cannot remove durable workflow compensation history';
    END IF;
END
$$;
-- +goose StatementEnd

ALTER TABLE idempotency_record
    DROP CONSTRAINT idempotency_record_compensated_event_fk,
    DROP CONSTRAINT idempotency_record_compensated_ref,
    DROP COLUMN compensated_by_event_ref,
    DROP CONSTRAINT idempotency_record_status_allowed,
    ADD CONSTRAINT idempotency_record_status_allowed CHECK (
        status IN ('RESERVED', 'COMPLETED', 'TOMBSTONE')
    ),
    DROP CONSTRAINT idempotency_record_expires_after_created,
    ADD CONSTRAINT idempotency_record_expires_after_created CHECK (
        (status = 'TOMBSTONE' AND expires_at IS NULL)
        OR (status <> 'TOMBSTONE' AND expires_at > created_at)
    );

DROP TABLE workflow_compensation_operation;
DROP TRIGGER workflow_compensation_event_forbid_mutation ON workflow_compensation_event;
REVOKE ALL ON workflow_compensation_event FROM hcmnext_app;
DROP POLICY tenant_isolation ON workflow_compensation_event;
ALTER TABLE workflow_compensation_event NO FORCE ROW LEVEL SECURITY;
ALTER TABLE workflow_compensation_event DISABLE ROW LEVEL SECURITY;
DROP TABLE workflow_compensation_event;
