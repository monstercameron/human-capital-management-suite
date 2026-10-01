-- AGENTP-011/012: bind retained persona output to its exact accepted run.
-- +goose Up

ALTER TABLE persona_final_outputs
    ADD COLUMN admission_id text,
    ADD COLUMN run_id text,
    ADD CONSTRAINT persona_final_outputs_run_binding_pair
		CHECK ((admission_id IS NULL AND run_id IS NULL) OR
		       (admission_id IS NOT NULL AND run_id IS NOT NULL AND btrim(admission_id) <> '' AND btrim(run_id) <> ''));

-- +goose Down
ALTER TABLE persona_final_outputs
    DROP CONSTRAINT persona_final_outputs_run_binding_pair,
    DROP COLUMN run_id,
    DROP COLUMN admission_id;
