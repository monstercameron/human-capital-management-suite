-- WF-RUN-040 follow-up: workflow_execution_context (00291) is declared
-- append-only in definitions/storage/storage-disposition.yaml, and the grants
-- already withhold UPDATE and DELETE from the application role, but the table
-- carried no forbid_mutation trigger, so a privileged session could still
-- rewrite a pinned context. TestTodo_STORE_001_Integration requires every
-- append-only table to carry the trigger; this migration adds it.

-- +goose Up

CREATE TRIGGER workflow_execution_context_forbid_mutation
    BEFORE UPDATE OR DELETE ON workflow_execution_context
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00298 is irreversible: migrations 00279-00297 already broke the rollback chain, so this migration keeps that true rather than claiming a reversibility no rollback can ever reach'; END $$;
-- +goose StatementEnd
