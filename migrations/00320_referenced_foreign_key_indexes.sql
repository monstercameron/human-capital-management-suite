-- PERFOPT-005: child-side indexes for referenced foreign keys added after the
-- original index audit. Shared key columns cover all matching constraints.

-- +goose Up
CREATE INDEX ix_fx_quote_revision_parent ON fx_quote_revision (tenant_id, parent_quote_id, parent_digest);
CREATE INDEX ix_job_position_job ON job_position (tenant_id, job_ref);
CREATE INDEX ix_job_position_legal_entity ON job_position (tenant_id, legal_entity_ref);
CREATE INDEX ix_job_position_organization ON job_position (tenant_id, organization_ref);
CREATE INDEX ix_leave_medical_detail_record ON leave_medical_detail (tenant_id, record_id, record_revision);
CREATE INDEX ix_leave_record_request ON leave_record (tenant_id, request_id, request_revision);
CREATE INDEX ix_ledger_payload_disposition_event ON ledger_payload_disposition (tenant_id, event_id);
CREATE INDEX ix_ledger_payload_disposition_link ON ledger_payload_disposition (tenant_id, link_id);
CREATE INDEX ix_legal_evaluation_binding_receipt ON legal_evaluation_binding (tenant_id, receipt_ref, receipt_digest);
CREATE INDEX ix_legal_hold_acknowledgement_hold ON legal_hold_acknowledgement (tenant_id, hold_id);
CREATE INDEX ix_merit_compensation_intent_emission_cycle ON merit_compensation_intent_emission (tenant_id, cycle_id, cycle_revision);
CREATE INDEX ix_operator_bypass_obligation_key ON operator_bypass_obligation (tenant_id, idempotency_key);
CREATE INDEX ix_organization_unit_legal_entity ON organization_unit (tenant_id, legal_entity_ref);
CREATE INDEX ix_position_occupancy_assignment ON position_occupancy (tenant_id, assignment_ref);
CREATE INDEX ix_position_occupancy_worker ON position_occupancy (tenant_id, worker_ref);
CREATE INDEX ix_worker_skill_evidence_supersedes ON worker_skill_evidence (tenant_id, supersedes_evidence_id, worker_ref, skill_ref);
CREATE INDEX ix_workflow_signal_disposition_subscription ON workflow_signal_disposition (tenant_id, subscription_id);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00320 is irreversible: dropping the covered foreign-key indexes would restore a known production performance defect'; END $$;
-- +goose StatementEnd
