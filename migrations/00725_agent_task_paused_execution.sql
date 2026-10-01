-- Durable pre-call pauses retain an execution event without effect evidence.
-- +goose Up
ALTER TABLE agent_task_event DROP CONSTRAINT agent_task_event_evidence_shape;
ALTER TABLE agent_task_event
    ADD CONSTRAINT agent_task_event_evidence_shape CHECK (
        event_sequence > 0 AND btrim(outcome) <> '' AND
        event_type IN ('PLAN_REVISION', 'PLAN_CONFIRMATION', 'APPROVAL_REQUESTED', 'APPROVAL_OUTCOME', 'STEP_EXECUTION', 'STEP_VERIFICATION', 'STEP_EFFECT') AND
        (plan_snapshot IS NULL OR (
            jsonb_typeof(plan_snapshot) = 'object' AND
            plan_snapshot->>'revision' = plan_revision::text AND
            plan_snapshot->>'digest' = plan_digest AND
            jsonb_typeof(plan_snapshot->'steps') = 'array'
        )) AND
        (evidence_ref IS NULL OR btrim(evidence_ref) <> '') AND
        (evidence_digest IS NULL OR btrim(evidence_digest) <> '') AND
        (
            (event_type IN ('PLAN_REVISION', 'PLAN_CONFIRMATION') AND
                plan_revision > 0 AND plan_digest IS NOT NULL AND btrim(plan_digest) <> '' AND
                step_id IS NULL AND step_type IS NULL AND side_effect_tier IS NULL AND approval_digest IS NULL AND
                evidence_ref IS NULL AND evidence_digest IS NULL)
            OR
            (event_type IN ('APPROVAL_REQUESTED', 'APPROVAL_OUTCOME') AND
                step_id IS NOT NULL AND btrim(step_id) <> '' AND approval_digest IS NOT NULL AND btrim(approval_digest) <> '' AND
                ((plan_revision IS NULL AND plan_digest IS NULL AND step_type IS NULL AND side_effect_tier IS NULL) OR
                 (plan_revision > 0 AND plan_digest IS NOT NULL AND btrim(plan_digest) <> '' AND
                  step_type IS NOT NULL AND step_type IN ('READ','ANALYZE','DRAFT','COMMUNICATE','SUBMIT','VERIFY','ASK_USER','WAIT') AND
                  side_effect_tier IS NOT NULL AND side_effect_tier BETWEEN 3 AND 4)) AND
                plan_snapshot IS NULL AND evidence_ref IS NULL AND evidence_digest IS NULL)
            OR
            (event_type = 'STEP_EXECUTION' AND plan_revision > 0 AND plan_digest IS NOT NULL AND btrim(plan_digest) <> '' AND
                step_id IS NOT NULL AND btrim(step_id) <> '' AND step_type IS NOT NULL AND step_type IN ('READ','ANALYZE','DRAFT','COMMUNICATE','SUBMIT','VERIFY','ASK_USER','WAIT') AND
                side_effect_tier IS NOT NULL AND side_effect_tier BETWEEN 0 AND 4 AND approval_digest IS NULL AND plan_snapshot IS NULL AND outcome IN ('SUCCEEDED','FAILED','PAUSED') AND (outcome <> 'PAUSED' OR (evidence_ref IS NULL AND evidence_digest IS NULL)))
            OR
            (event_type = 'STEP_VERIFICATION' AND plan_revision > 0 AND plan_digest IS NOT NULL AND btrim(plan_digest) <> '' AND
                step_id IS NOT NULL AND btrim(step_id) <> '' AND step_type IS NOT NULL AND step_type = 'VERIFY' AND side_effect_tier IS NOT NULL AND side_effect_tier BETWEEN 0 AND 4 AND
                approval_digest IS NULL AND plan_snapshot IS NULL AND outcome IN ('VERIFIED','FAILED'))
            OR
            (event_type = 'STEP_EFFECT' AND plan_revision > 0 AND plan_digest IS NOT NULL AND btrim(plan_digest) <> '' AND
                step_id IS NOT NULL AND btrim(step_id) <> '' AND step_type IS NOT NULL AND step_type IN ('READ','ANALYZE','DRAFT','COMMUNICATE','SUBMIT','VERIFY','ASK_USER','WAIT') AND
                side_effect_tier IS NOT NULL AND side_effect_tier BETWEEN 3 AND 4 AND approval_digest IS NOT NULL AND btrim(approval_digest) <> '' AND
                plan_snapshot IS NULL AND outcome IN ('SUCCEEDED','FAILED'))
        )
    );

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM agent_task_event WHERE event_type='STEP_EXECUTION' AND outcome='PAUSED') THEN
        RAISE EXCEPTION 'cannot remove retained paused step execution evidence';
    END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE agent_task_event DROP CONSTRAINT agent_task_event_evidence_shape;
ALTER TABLE agent_task_event
    ADD CONSTRAINT agent_task_event_evidence_shape CHECK (
        event_sequence > 0 AND btrim(outcome) <> '' AND
        event_type IN ('PLAN_REVISION', 'PLAN_CONFIRMATION', 'APPROVAL_REQUESTED', 'APPROVAL_OUTCOME', 'STEP_EXECUTION', 'STEP_VERIFICATION', 'STEP_EFFECT') AND
        (plan_snapshot IS NULL OR (
            jsonb_typeof(plan_snapshot) = 'object' AND
            plan_snapshot->>'revision' = plan_revision::text AND
            plan_snapshot->>'digest' = plan_digest AND
            jsonb_typeof(plan_snapshot->'steps') = 'array'
        )) AND
        (evidence_ref IS NULL OR btrim(evidence_ref) <> '') AND
        (evidence_digest IS NULL OR btrim(evidence_digest) <> '') AND
        (
            (event_type IN ('PLAN_REVISION', 'PLAN_CONFIRMATION') AND
                plan_revision > 0 AND plan_digest IS NOT NULL AND btrim(plan_digest) <> '' AND
                step_id IS NULL AND step_type IS NULL AND side_effect_tier IS NULL AND approval_digest IS NULL AND
                evidence_ref IS NULL AND evidence_digest IS NULL)
            OR
            (event_type IN ('APPROVAL_REQUESTED', 'APPROVAL_OUTCOME') AND
                step_id IS NOT NULL AND btrim(step_id) <> '' AND approval_digest IS NOT NULL AND btrim(approval_digest) <> '' AND
                ((plan_revision IS NULL AND plan_digest IS NULL AND step_type IS NULL AND side_effect_tier IS NULL) OR
                 (plan_revision > 0 AND plan_digest IS NOT NULL AND btrim(plan_digest) <> '' AND
                  step_type IS NOT NULL AND step_type IN ('READ','ANALYZE','DRAFT','COMMUNICATE','SUBMIT','VERIFY','ASK_USER','WAIT') AND
                  side_effect_tier IS NOT NULL AND side_effect_tier BETWEEN 3 AND 4)) AND
                plan_snapshot IS NULL AND evidence_ref IS NULL AND evidence_digest IS NULL)
            OR
            (event_type = 'STEP_EXECUTION' AND plan_revision > 0 AND plan_digest IS NOT NULL AND btrim(plan_digest) <> '' AND
                step_id IS NOT NULL AND btrim(step_id) <> '' AND step_type IS NOT NULL AND step_type IN ('READ','ANALYZE','DRAFT','COMMUNICATE','SUBMIT','VERIFY','ASK_USER','WAIT') AND
                side_effect_tier IS NOT NULL AND side_effect_tier BETWEEN 0 AND 4 AND approval_digest IS NULL AND plan_snapshot IS NULL AND outcome IN ('SUCCEEDED','FAILED'))
            OR
            (event_type = 'STEP_VERIFICATION' AND plan_revision > 0 AND plan_digest IS NOT NULL AND btrim(plan_digest) <> '' AND
                step_id IS NOT NULL AND btrim(step_id) <> '' AND step_type IS NOT NULL AND step_type = 'VERIFY' AND side_effect_tier IS NOT NULL AND side_effect_tier BETWEEN 0 AND 4 AND
                approval_digest IS NULL AND plan_snapshot IS NULL AND outcome IN ('VERIFIED','FAILED'))
            OR
            (event_type = 'STEP_EFFECT' AND plan_revision > 0 AND plan_digest IS NOT NULL AND btrim(plan_digest) <> '' AND
                step_id IS NOT NULL AND btrim(step_id) <> '' AND step_type IS NOT NULL AND step_type IN ('READ','ANALYZE','DRAFT','COMMUNICATE','SUBMIT','VERIFY','ASK_USER','WAIT') AND
                side_effect_tier IS NOT NULL AND side_effect_tier BETWEEN 3 AND 4 AND approval_digest IS NOT NULL AND btrim(approval_digest) <> '' AND
                plan_snapshot IS NULL AND outcome IN ('SUCCEEDED','FAILED'))
        )
    );
