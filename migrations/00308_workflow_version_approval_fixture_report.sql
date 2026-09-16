-- WF-COMP-006: an activation approval carries the fixture evidence it rests on.
--
-- Until this migration a workflow_version_approval row recorded tests_passed
-- as a caller-asserted boolean, and serve recorded one at boot under a
-- configured release approver with tests_passed = true. Approval is now a
-- governed operator action (hcmnext workflow-version approve) that re-runs the
-- version's declared conformance fixtures in-process and records the sealed
-- report (internal/workflow/releasefixture) with the approval:
--
--   fixture_report_digest  the report's own content digest
--   fixture_report         the report: format, workflow, compiled-plan and
--                          record digests, runner, instant and one result per
--                          declared fixture
--
-- internal/data/workflowversionstore re-verifies the stored report against
-- the version on every first activation of a DRAFT and refuses one without it.
-- Rows written before this migration keep both columns NULL; the table stays
-- append-only (forbid_mutation from 00292), so no existing approval is edited.

-- +goose Up

ALTER TABLE workflow_version_approval
    ADD COLUMN fixture_report_digest text,
    ADD COLUMN fixture_report        jsonb;

ALTER TABLE workflow_version_approval
    ADD CONSTRAINT workflow_version_approval_fixture_report_pair
        CHECK ((fixture_report IS NULL) = (fixture_report_digest IS NULL)),
    ADD CONSTRAINT workflow_version_approval_fixture_report_object
        CHECK (fixture_report IS NULL OR (jsonb_typeof(fixture_report) = 'object' AND fixture_report_digest <> '')),
    ADD CONSTRAINT workflow_version_approval_fixture_report_passed
        CHECK (fixture_report IS NULL OR tests_passed);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00308 is irreversible: migrations 00279-00302 already broke the rollback chain, so this migration keeps that true rather than claiming a reversibility no rollback can ever reach'; END $$;
-- +goose StatementEnd
