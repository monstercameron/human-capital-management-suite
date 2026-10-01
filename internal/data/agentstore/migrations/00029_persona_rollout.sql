-- AGENT-044 / AGENTP rollout plans, approvals, progress and exact installation refs.
-- +goose Up
ALTER TABLE persona_installations
    ADD COLUMN rollout_preview_ref text NOT NULL DEFAULT '',
    ADD COLUMN rollout_approval_ref text NOT NULL DEFAULT '';
ALTER TABLE persona_installations
    ADD CONSTRAINT persona_installations_rollout_refs_pair CHECK
    ((rollout_preview_ref = '') = (rollout_approval_ref = ''));

CREATE TABLE persona_version_rollout (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    rollout_id text NOT NULL CHECK (btrim(rollout_id) <> ''),
    plan_digest text NOT NULL CHECK (plan_digest ~ '^sha256:[0-9a-f]{64}$'),
    plan jsonb NOT NULL CHECK (jsonb_typeof(plan) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, rollout_id),
    UNIQUE (tenant_id, plan_digest)
);
CREATE TRIGGER persona_version_rollout_immutable BEFORE UPDATE OR DELETE ON persona_version_rollout
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

CREATE TABLE persona_version_rollout_progress (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    rollout_id text NOT NULL,
    revision bigint NOT NULL CHECK (revision > 0),
    cursor integer NOT NULL CHECK (cursor >= 0),
    stage text NOT NULL CHECK (stage IN ('PREVIEWED','APPROVED','CANARY_COMPLETE','COMPLETE')),
    approver_id text NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, rollout_id),
    FOREIGN KEY (tenant_id, rollout_id) REFERENCES persona_version_rollout(tenant_id, rollout_id)
);
CREATE INDEX persona_version_rollout_progress_stage ON persona_version_rollout_progress (tenant_id, stage, rollout_id);
ALTER TABLE persona_version_rollout_progress ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_version_rollout_progress FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_version_rollout_progress
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

CREATE TABLE persona_version_rollout_approval (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    approval_id text NOT NULL CHECK (btrim(approval_id) <> ''),
    rollout_id text NOT NULL,
    plan_digest text NOT NULL CHECK (plan_digest ~ '^sha256:[0-9a-f]{64}$'),
    actor_id text NOT NULL CHECK (btrim(actor_id) <> ''),
    approved_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, approval_id),
    UNIQUE (tenant_id, rollout_id),
    FOREIGN KEY (tenant_id, rollout_id) REFERENCES persona_version_rollout(tenant_id, rollout_id)
);
CREATE TRIGGER persona_version_rollout_approval_immutable BEFORE UPDATE OR DELETE ON persona_version_rollout_approval
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
ALTER TABLE persona_version_rollout ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_version_rollout FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_version_rollout
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE persona_version_rollout_approval ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_version_rollout_approval FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_version_rollout_approval
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON persona_version_rollout, persona_version_rollout_approval TO hcmnext_agent_app;
GRANT SELECT, INSERT, UPDATE ON persona_version_rollout_progress TO hcmnext_agent_app;
GRANT UPDATE (rollout_preview_ref, rollout_approval_ref) ON persona_installations TO hcmnext_agent_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM persona_version_rollout_approval) OR EXISTS (SELECT 1 FROM persona_version_rollout_progress) OR EXISTS (SELECT 1 FROM persona_version_rollout) THEN
        RAISE EXCEPTION 'cannot remove retained persona rollout state';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE persona_version_rollout_approval;
DROP TABLE persona_version_rollout_progress;
DROP TABLE persona_version_rollout;
ALTER TABLE persona_installations DROP CONSTRAINT persona_installations_rollout_refs_pair;
ALTER TABLE persona_installations DROP COLUMN rollout_preview_ref, DROP COLUMN rollout_approval_ref;
