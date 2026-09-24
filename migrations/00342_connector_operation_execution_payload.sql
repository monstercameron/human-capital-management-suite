-- Owner: connectivity lane. Phase: Gate B.
-- storage-disposition: connector_operation.execution_payload | tenant-scoped durable replay input for a planned provider write | local PostgreSQL | worker restart recovery of PLANNED/QUEUED operations | tenant-scoped.

-- +goose Up

ALTER TABLE connector_operation
    ADD COLUMN execution_payload jsonb;

COMMENT ON COLUMN connector_operation.execution_payload IS
	'Tenant-scoped immutable PlanRequest snapshot used to restore a queued connector operation after worker restart; contains mapped payload bytes but never credential bytes.';

-- +goose StatementBegin
CREATE FUNCTION forbid_connector_operation_execution_payload_mutation()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.execution_payload IS DISTINCT FROM OLD.execution_payload THEN
        RAISE EXCEPTION 'connector operation execution payload is immutable';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER connector_operation_execution_payload_immutable
    BEFORE UPDATE ON connector_operation
    FOR EACH ROW EXECUTE FUNCTION forbid_connector_operation_execution_payload_mutation();

-- +goose Down

DROP TRIGGER connector_operation_execution_payload_immutable ON connector_operation;
DROP FUNCTION forbid_connector_operation_execution_payload_mutation();
ALTER TABLE connector_operation
    DROP COLUMN execution_payload;
