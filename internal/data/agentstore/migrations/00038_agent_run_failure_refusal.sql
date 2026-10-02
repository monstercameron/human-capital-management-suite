-- AGENTUX-036: store only closed-list operator diagnostics for failed runs.
-- +goose Up

ALTER TABLE agent_run_execution
    ADD COLUMN failure_gate text NOT NULL DEFAULT '',
    ADD COLUMN failure_owner text NOT NULL DEFAULT '',
    ADD COLUMN failure_location text NOT NULL DEFAULT '';

ALTER TABLE agent_run_execution
    ADD CONSTRAINT agent_run_failure_refusal_gate CHECK (failure_gate='' OR failure_gate IN (
        'authority','grant','installation','audience','budget','model_route','model_call','model_output',
        'tool_scope','tool_call','output_grounding','output_schema','delivery_audience','delivery_write',
        'deadline','stopped'
    )),
    ADD CONSTRAINT agent_run_failure_refusal_triplet CHECK ((failure_gate='') = (failure_owner='' AND failure_location=''));

-- +goose Down
ALTER TABLE agent_run_execution DROP CONSTRAINT agent_run_failure_refusal_triplet;
ALTER TABLE agent_run_execution DROP CONSTRAINT agent_run_failure_refusal_gate;
ALTER TABLE agent_run_execution DROP COLUMN failure_location, DROP COLUMN failure_owner, DROP COLUMN failure_gate;
