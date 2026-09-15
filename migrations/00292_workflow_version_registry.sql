-- WF-COMP-006 / WF-RUN-035: the durable compiled workflow version registry.
--
-- Until this migration serve published and activated every workflow version
-- into an in-memory version.Registry on each boot and approved the activation
-- itself (Authorized: true). A restart lost the activation authority, the
-- approval history and any quarantine, and nothing could prove which approver
-- had put a version into service.
--
-- These are platform catalogs, like industry_pack_manifest (00082): a compiled
-- workflow version is governed reference content every tenant's instances pin
-- by digest, not tenant data, so the tables carry no tenant_id.
--
--   workflow_compiled_version  one row per compiled-plan digest: the immutable
--                              record (content digest checked on every read)
--                              and its current lifecycle status. Only status
--                              and the approval history may change, and at
--                              most one version per workflow is ACTIVE.
--   workflow_version_transition append-only lifecycle history, one row per
--                              governed status change.
--   workflow_version_approval  append-only activation approvals. Activation
--                              through internal/data/workflowversionstore
--                              requires one, granted against the exact
--                              compiled-plan digest by an approver who is not
--                              the publisher.

-- +goose Up

CREATE TABLE workflow_compiled_version (
    compiled_plan_digest text        PRIMARY KEY,
    workflow_id          text        NOT NULL,
    semantic_version     text        NOT NULL,
    record_digest        text        NOT NULL,
    record               jsonb       NOT NULL,
    status               text        NOT NULL,
    published_at         timestamptz NOT NULL,
    published_by         text        NOT NULL,
    updated_at           timestamptz NOT NULL,
    CONSTRAINT workflow_compiled_version_status CHECK (status IN ('DRAFT', 'ACTIVE', 'QUARANTINED', 'RETIRED')),
    CONSTRAINT workflow_compiled_version_digests CHECK (compiled_plan_digest <> '' AND record_digest <> '')
);

CREATE UNIQUE INDEX workflow_compiled_version_one_active
    ON workflow_compiled_version (workflow_id) WHERE status = 'ACTIVE';
CREATE INDEX workflow_compiled_version_by_workflow
    ON workflow_compiled_version (workflow_id, published_at);

-- +goose StatementBegin
CREATE FUNCTION workflow_compiled_version_identity_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.workflow_id <> OLD.workflow_id OR NEW.semantic_version <> OLD.semantic_version
        OR NEW.record_digest <> OLD.record_digest OR NEW.published_at <> OLD.published_at
        OR NEW.published_by <> OLD.published_by THEN
        RAISE EXCEPTION 'workflow_compiled_version % identity is immutable', OLD.compiled_plan_digest;
    END IF;
    IF OLD.status = 'RETIRED' AND NEW.status <> 'RETIRED' THEN
        RAISE EXCEPTION 'workflow_compiled_version % is retired and cannot change status', OLD.compiled_plan_digest;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER workflow_compiled_version_identity
    BEFORE UPDATE ON workflow_compiled_version
    FOR EACH ROW EXECUTE FUNCTION workflow_compiled_version_identity_guard();
CREATE TRIGGER workflow_compiled_version_no_delete
    BEFORE DELETE ON workflow_compiled_version
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

CREATE TABLE workflow_version_transition (
    compiled_plan_digest text        NOT NULL REFERENCES workflow_compiled_version (compiled_plan_digest),
    sequence             integer     NOT NULL,
    status               text        NOT NULL,
    approved_by          text        NOT NULL,
    authority            text        NOT NULL,
    reason               text        NOT NULL,
    approved_at          timestamptz NOT NULL,
    PRIMARY KEY (compiled_plan_digest, sequence),
    CONSTRAINT workflow_version_transition_sequence_positive CHECK (sequence > 0),
    CONSTRAINT workflow_version_transition_status CHECK (status IN ('ACTIVE', 'QUARANTINED', 'RETIRED'))
);

CREATE TRIGGER workflow_version_transition_append_only
    BEFORE UPDATE OR DELETE ON workflow_version_transition
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

CREATE TABLE workflow_version_approval (
    approval_id          uuid        PRIMARY KEY,
    compiled_plan_digest text        NOT NULL REFERENCES workflow_compiled_version (compiled_plan_digest),
    reviewed_plan_digest text        NOT NULL,
    approved_by          text        NOT NULL,
    authority            text        NOT NULL,
    reason               text        NOT NULL,
    tests_passed         boolean     NOT NULL,
    fixture_refs         jsonb       NOT NULL,
    approved_at          timestamptz NOT NULL,
    CONSTRAINT workflow_version_approval_approver CHECK (approved_by <> ''),
    CONSTRAINT workflow_version_approval_fixtures_array CHECK (jsonb_typeof(fixture_refs) = 'array')
);

CREATE INDEX workflow_version_approval_by_digest
    ON workflow_version_approval (compiled_plan_digest, approved_at);

CREATE TRIGGER workflow_version_approval_append_only
    BEFORE UPDATE OR DELETE ON workflow_version_approval
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

GRANT SELECT, INSERT, UPDATE ON workflow_compiled_version TO hcmnext_app;
GRANT SELECT, INSERT ON workflow_version_transition, workflow_version_approval TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00292 is irreversible: migrations 00279-00291 already broke the rollback chain, so this migration keeps that true rather than claiming a reversibility no rollback can ever reach'; END $$;
-- +goose StatementEnd
