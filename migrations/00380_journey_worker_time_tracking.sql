-- Backend half of WTIME-001 on the worker record: the exemption status, the
-- time capture ("hour tracking") mode and the time profile reference that
-- internal/domains/timeprofile's vocabulary already defines but journey_worker
-- had no columns for.
--
-- WTIME-001 asks for a versioned TimeProfile carrying capture mode, pay basis,
-- exemption status and worker category as independently asserted fields, none
-- derived from another. journey_worker already carries pay basis
-- (migrations/00023) and worker category is a mapping the Go layer derives
-- from worker_type (internal/data/workforce.WorkerCategoryFor), so it needs no
-- column of its own here. What is genuinely missing from the row is:
--
--   * exemption_status  -- the overtime classification (NON_EXEMPT, EXEMPT,
--     SALARIED_NON_EXEMPT, NOT_APPLICABLE). It is its own field rather than
--     read off pay_basis, because a salaried worker can be exempt or
--     non-exempt and a hourly worker is not automatically non-exempt.
--   * time_capture_mode  -- how this worker's time is recorded (PUNCH,
--     DURATION, EXCEPTION_ONLY, NONE). This is the "hour tracking" type the
--     journey worker object page asks for; it is a different axis from
--     time_type (FULL_TIME/PART_TIME, migrations/00316), which says nothing
--     about how time is captured.
--   * time_profile_ref  -- the named time profile this worker's capture mode
--     and exemption were resolved from. It is a free-text reference rather
--     than a foreign key because the profile catalog (WTIME-002's template
--     registry) is not owned by this table; journey_worker only records which
--     profile a worker was placed under.
--
-- All three are nullable, exactly as migrations/00316 left employment_type,
-- time_type, company, business_unit, cost_center and work_arrangement
-- nullable: NULL means nobody asserted the fact yet, which keeps "not
-- reported" distinguishable from an invented default for every row already
-- written before this migration ran.
--
-- The two closed vocabularies are the tokens
-- internal/domains/timeprofile/vocabulary.go already declares
-- (ExemptionStatus, CaptureMode) -- this migration does not invent a second
-- spelling of either. time_profile_ref is a name, not a token, so it is only
-- required not to be blank padding, the same rule migrations/00316 applies to
-- company, business_unit and cost_center.
--
-- journey_worker stays append-only: this is DDL, not a row mutation, and
-- nothing here touches the journey_worker_append_only trigger or the
-- REVOKE of UPDATE/DELETE migrations/00023 already put in place. A worker
-- whose exemption or capture mode changes gets a new row on the next
-- revision, exactly like every other fact on this table.
--
-- worker_type itself carries no CHECK constraint (migrations/00023 and
-- 00316 both left it free text), so AGENCY_TEMP needs no schema change here;
-- internal/data/workforce.WorkerCategoryFor maps it (and PLATFORM_WORKER)
-- onto internal/domains/timeprofile's WorkerCategory alongside the four
-- tokens journey_worker's callers write today (EMPLOYEE, CONTRACTOR, INTERN,
-- TEMPORARY).

-- +goose Up

ALTER TABLE journey_worker
    ADD COLUMN exemption_status   text,
    ADD COLUMN time_capture_mode  text,
    ADD COLUMN time_profile_ref   text,

    ADD CONSTRAINT journey_worker_exemption_status_allowed CHECK (
        exemption_status IS NULL OR exemption_status IN (
            'NON_EXEMPT', 'EXEMPT', 'SALARIED_NON_EXEMPT', 'NOT_APPLICABLE'
        )
    ),
    ADD CONSTRAINT journey_worker_time_capture_mode_allowed CHECK (
        time_capture_mode IS NULL OR time_capture_mode IN (
            'PUNCH', 'DURATION', 'EXCEPTION_ONLY', 'NONE'
        )
    ),
    ADD CONSTRAINT journey_worker_time_profile_ref_present CHECK (
        time_profile_ref IS NULL OR length(btrim(time_profile_ref)) > 0
    );

-- +goose Down
ALTER TABLE journey_worker
    DROP CONSTRAINT journey_worker_exemption_status_allowed,
    DROP CONSTRAINT journey_worker_time_capture_mode_allowed,
    DROP CONSTRAINT journey_worker_time_profile_ref_present,
    DROP COLUMN exemption_status,
    DROP COLUMN time_capture_mode,
    DROP COLUMN time_profile_ref;
