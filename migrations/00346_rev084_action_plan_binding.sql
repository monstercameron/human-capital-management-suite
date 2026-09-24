-- REV-084-02: preserve the durable acceptance and bind it to the exact
-- transaction plan which commits the accepted action.

-- +goose Up

ALTER TABLE idempotency_record
    ADD COLUMN action_plan_binding_ref content_digest;

ALTER TABLE idempotency_record
    DROP CONSTRAINT idempotency_record_reserved_has_no_identity,
    DROP CONSTRAINT idempotency_record_completed_has_identity;

ALTER TABLE idempotency_record
    ADD CONSTRAINT idempotency_record_reserved_has_no_identity CHECK (
        status <> 'RESERVED' OR (
            result_ref IS NULL AND event_ref IS NULL
            AND effect_identity IS NULL AND evidence_id IS NULL
            AND action_plan_binding_ref IS NULL
        )
    ),
    ADD CONSTRAINT idempotency_record_completed_has_identity CHECK (
        status <> 'COMPLETED' OR (
            result_ref IS NOT NULL OR event_ref IS NOT NULL
            OR effect_identity IS NOT NULL OR evidence_id IS NOT NULL
            OR action_plan_binding_ref IS NOT NULL
        )
    );

CREATE TABLE intent_accepted_action (
    tenant_id            tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    decision_id          uuid NOT NULL,
    intent_id            uuid NOT NULL,
    action_id            semantic_key NOT NULL,
    proposal_revision_id semantic_key NOT NULL,
    proposal_digest      content_digest NOT NULL,
    accepted_by          semantic_key NOT NULL,
    accepted_at          timestamptz NOT NULL,
    idempotency_key      semantic_key NOT NULL,
    acceptance_digest    content_digest NOT NULL,
    recorded_at          timestamptz NOT NULL DEFAULT clock_timestamp(),

    PRIMARY KEY (tenant_id, decision_id),
    UNIQUE (tenant_id, action_id, idempotency_key),
    CONSTRAINT intent_accepted_action_decision
        FOREIGN KEY (tenant_id, decision_id) REFERENCES intent_decision (tenant_id, decision_id),
    CONSTRAINT intent_accepted_action_intent
        FOREIGN KEY (tenant_id, intent_id) REFERENCES intent_instance (tenant_id, intent_id)
);

CREATE OR REPLACE TRIGGER intent_accepted_action_append_only
    BEFORE UPDATE OR DELETE ON intent_accepted_action
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

CREATE TABLE intent_action_plan_binding (
    tenant_id            tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    decision_id          uuid NOT NULL,
    plan_id              uuid NOT NULL,
    action_id            semantic_key NOT NULL,
    intent_id            uuid NOT NULL,
    proposal_revision_id semantic_key NOT NULL,
    proposal_digest      content_digest NOT NULL,
    accepted_by          semantic_key NOT NULL,
    accepted_at          timestamptz NOT NULL,
    idempotency_key      semantic_key NOT NULL,
    plan_digest          content_digest NOT NULL,
    binding_digest       content_digest NOT NULL,
    binding_payload      jsonb NOT NULL CHECK (jsonb_typeof(binding_payload) = 'object'),
    recorded_at          timestamptz NOT NULL DEFAULT clock_timestamp(),

    PRIMARY KEY (tenant_id, decision_id, plan_id),
    CONSTRAINT intent_action_plan_binding_acceptance
        FOREIGN KEY (tenant_id, decision_id) REFERENCES intent_accepted_action (tenant_id, decision_id),
    CONSTRAINT intent_action_plan_binding_plan
        FOREIGN KEY (tenant_id, plan_id) REFERENCES transaction_plan (tenant_id, plan_id)
);

CREATE OR REPLACE TRIGGER intent_action_plan_binding_append_only
    BEFORE UPDATE OR DELETE ON intent_action_plan_binding
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

-- Both records are durable, tenant-local evidence. Binding insertion occurs
-- inside the plan commit transaction, before the TX-006 result stores its
-- binding digest reference.
ALTER TABLE intent_accepted_action ENABLE ROW LEVEL SECURITY;
ALTER TABLE intent_accepted_action FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON intent_accepted_action
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE intent_action_plan_binding ENABLE ROW LEVEL SECURITY;
ALTER TABLE intent_action_plan_binding FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON intent_action_plan_binding
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

REVOKE UPDATE, DELETE ON intent_accepted_action, intent_action_plan_binding FROM PUBLIC;
GRANT SELECT, INSERT ON intent_accepted_action, intent_action_plan_binding TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON idempotency_record TO hcmnext_app;

-- +goose Down
REVOKE ALL ON intent_action_plan_binding, intent_accepted_action FROM hcmnext_app;
DROP POLICY tenant_isolation ON intent_action_plan_binding;
DROP POLICY tenant_isolation ON intent_accepted_action;
DROP TABLE intent_action_plan_binding;
DROP TABLE intent_accepted_action;
ALTER TABLE idempotency_record
    DROP CONSTRAINT idempotency_record_reserved_has_no_identity,
    DROP CONSTRAINT idempotency_record_completed_has_identity,
    DROP COLUMN action_plan_binding_ref;
ALTER TABLE idempotency_record
    ADD CONSTRAINT idempotency_record_reserved_has_no_identity CHECK (
        status <> 'RESERVED' OR (
            result_ref IS NULL AND event_ref IS NULL AND effect_identity IS NULL AND evidence_id IS NULL
        )
    ),
    ADD CONSTRAINT idempotency_record_completed_has_identity CHECK (
        status <> 'COMPLETED' OR (
            result_ref IS NOT NULL OR event_ref IS NOT NULL OR effect_identity IS NOT NULL OR evidence_id IS NOT NULL
        )
    );
