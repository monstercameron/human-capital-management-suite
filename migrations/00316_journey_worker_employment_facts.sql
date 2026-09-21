-- Employment facts the worker object page asks journey_worker for.
--
-- The person page already renders a worker's placement (job code, grade, org
-- unit, position, location, pay zone) straight off this row. Six further
-- facts -- the employment and time types, the employing company, the business
-- unit, the cost center and the work arrangement -- had no column here at
-- all, so the page had nowhere to read them from and rendered "not reported"
-- for every worker, seeded or not.
--
-- They belong on journey_worker rather than on the migrations/00011
-- employment/assignment aggregates, and the reason is the read path, not
-- convenience. The object page reads this table (internal/data/workforce's
-- Store.List, through the journey engine's listing); it never reads the
-- bitemporal aggregates, which exist for promotion commits. Storing these
-- facts only on the aggregates would have meant building a second worker read
-- across the engine, transport and authorization layers -- precisely the
-- "second, ungoverned way to read worker state" the journey Worker message
-- already warns against -- to surface facts the listing row is the natural
-- home for. The aggregates stay the authority for what a promotion commits
-- against; this row stays the authority for what the journey surface records
-- about a worker it created.
--
-- The columns are nullable, exactly as migrations/00070 added job_title and
-- the profile photo references. NULL means nobody asserted the fact, which is
-- what keeps a genuinely absent fact absent: the page's missing-field
-- disclosure still collapses it rather than showing an empty string that
-- pretends to be an answer. A NOT NULL DEFAULT would have invented a value
-- for every row already written.
--
-- This is DDL, not a row mutation: journey_worker stays append-only. The
-- journey_worker_append_only trigger and the REVOKE of UPDATE/DELETE are
-- untouched, and nothing here gives any writer a path to change a stored row.
-- A correction is still a new row, never an edit.
--
-- employment_type is the Regular/Fixed-term axis and is deliberately not the
-- same question as worker_type (EMPLOYEE/CONTRACTOR/INTERN/TEMPORARY), which
-- this table already carries: a fixed-term employee is still an employee.
-- time_type must agree with fte, which is why a PART_TIME token accompanies a
-- fractional fte in every row the demo seed writes.

-- +goose Up

ALTER TABLE journey_worker
    ADD COLUMN employment_type  text,
    ADD COLUMN time_type        text,
    ADD COLUMN company          text,
    ADD COLUMN business_unit    text,
    ADD COLUMN cost_center      text,
    ADD COLUMN work_arrangement text,
    -- The three vocabularies are closed. Free text would let a writer put a
    -- display string where a token belongs and leave the page translating
    -- whatever it found.
    ADD CONSTRAINT journey_worker_employment_type_allowed CHECK (
        employment_type IS NULL OR employment_type IN ('REGULAR', 'FIXED_TERM')
    ),
    ADD CONSTRAINT journey_worker_time_type_allowed CHECK (
        time_type IS NULL OR time_type IN ('FULL_TIME', 'PART_TIME')
    ),
    ADD CONSTRAINT journey_worker_work_arrangement_allowed CHECK (
        work_arrangement IS NULL OR work_arrangement IN ('ON_SITE', 'HYBRID', 'REMOTE')
    ),
    -- company, business_unit and cost_center are recorded names and codes,
    -- not tokens, so they are only required not to be blank padding: a
    -- present-but-empty value is the ambiguity the nullable column exists to
    -- avoid.
    ADD CONSTRAINT journey_worker_company_present CHECK (
        company IS NULL OR length(btrim(company)) > 0
    ),
    ADD CONSTRAINT journey_worker_business_unit_present CHECK (
        business_unit IS NULL OR length(btrim(business_unit)) > 0
    ),
    ADD CONSTRAINT journey_worker_cost_center_present CHECK (
        cost_center IS NULL OR length(btrim(cost_center)) > 0
    );

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00316 is irreversible: migrations 00279-00315 already broke the rollback chain, and dropping these columns would discard recorded employment facts rather than restore a reachable earlier schema'; END $$;
-- +goose StatementEnd
