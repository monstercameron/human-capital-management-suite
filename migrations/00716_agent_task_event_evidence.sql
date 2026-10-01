-- AGENT2-025: retain digest-bound historical plan identities and observed step evidence.

-- +goose Up

ALTER TABLE agent_task_event
    ADD COLUMN plan_snapshot jsonb,
    ADD COLUMN step_type text,
    ADD COLUMN side_effect_tier smallint,
    ADD COLUMN evidence_ref text,
    ADD COLUMN evidence_digest text;

-- Existing task rows can safely restore the identity of their still-current
-- plan because its revision and digest match. Retired revisions are never
-- reconstructed from current task state.
UPDATE agent_task_event AS event
SET plan_snapshot = jsonb_build_object(
    'revision', (task.plan->>'revision')::bigint,
    'digest', task.plan->>'digest',
    'steps', (
        SELECT jsonb_agg(step - ARRAY['approved', 'approval_digest', 'state', 'attempt', 'result_ref', 'verification_ref']::text[] ORDER BY ordinal)
        FROM jsonb_array_elements(task.plan->'steps') WITH ORDINALITY AS historical(step, ordinal)
    )
)
FROM agent_task AS task
WHERE event.tenant_id = task.tenant_id
  AND event.task_id = task.task_id
  AND event.event_type IN ('PLAN_REVISION', 'PLAN_CONFIRMATION')
  AND event.plan_revision = (task.plan->>'revision')::bigint
  AND event.plan_digest = task.plan->>'digest';

-- +goose StatementBegin
DO $$
DECLARE constraint_row record;
BEGIN
    FOR constraint_row IN
        SELECT conname
        FROM pg_constraint
        WHERE conrelid = 'agent_task_event'::regclass AND contype = 'c'
    LOOP
        EXECUTE format('ALTER TABLE agent_task_event DROP CONSTRAINT %I', constraint_row.conname);
    END LOOP;
END $$;
-- +goose StatementEnd

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

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM agent_task_event WHERE event_type IN ('STEP_EXECUTION', 'STEP_VERIFICATION', 'STEP_EFFECT')
        OR plan_snapshot IS NOT NULL OR step_type IS NOT NULL OR side_effect_tier IS NOT NULL OR evidence_ref IS NOT NULL OR evidence_digest IS NOT NULL) THEN
        RAISE EXCEPTION 'agent task evaluation evidence is retained; cannot drop';
    END IF;
END $$;
-- +goose StatementEnd

ALTER TABLE agent_task_event DROP CONSTRAINT agent_task_event_evidence_shape;
ALTER TABLE agent_task_event
    ADD CONSTRAINT agent_task_event_event_type_check CHECK (event_type IN ('PLAN_REVISION', 'PLAN_CONFIRMATION', 'APPROVAL_REQUESTED', 'APPROVAL_OUTCOME')),
    ADD CONSTRAINT agent_task_event_sequence_positive CHECK (event_sequence > 0),
    ADD CONSTRAINT agent_task_event_outcome_nonempty CHECK (btrim(outcome) <> ''),
    ADD CONSTRAINT agent_task_event_check CHECK (
        (event_type IN ('PLAN_REVISION', 'PLAN_CONFIRMATION') AND plan_revision IS NOT NULL AND plan_revision > 0 AND plan_digest IS NOT NULL AND btrim(plan_digest) <> '' AND step_id IS NULL AND approval_digest IS NULL)
        OR
        (event_type IN ('APPROVAL_REQUESTED', 'APPROVAL_OUTCOME') AND plan_revision IS NULL AND plan_digest IS NULL AND step_id IS NOT NULL AND btrim(step_id) <> '' AND approval_digest IS NOT NULL AND btrim(approval_digest) <> '')
    );
ALTER TABLE agent_task_event
    DROP COLUMN evidence_digest,
    DROP COLUMN evidence_ref,
    DROP COLUMN side_effect_tier,
    DROP COLUMN step_type,
    DROP COLUMN plan_snapshot;
