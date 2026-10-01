-- AGENTP-008 durable, tenant-isolated persona mention claims.
-- +goose Up
CREATE TABLE persona_invocations (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    invocation_id text NOT NULL CHECK (btrim(invocation_id) <> ''),
    conversation_id text NOT NULL CHECK (btrim(conversation_id) <> ''),
    thread_id text NOT NULL CHECK (btrim(thread_id) <> ''),
    post_id text NOT NULL CHECK (btrim(post_id) <> ''),
    invoker_id text NOT NULL CHECK (btrim(invoker_id) <> ''),
    persona_id text NOT NULL CHECK (btrim(persona_id) <> ''),
    persona_version text NOT NULL CHECK (btrim(persona_version) <> ''),
    installation_id text NOT NULL CHECK (btrim(installation_id) <> ''),
    mode text NOT NULL CHECK (mode = 'ON_BEHALF_OF'),
    skills jsonb NOT NULL CHECK (jsonb_typeof(skills)='object'),
    actor jsonb NOT NULL CHECK (jsonb_typeof(actor)='object'),
    grant_payload jsonb CHECK (grant_payload IS NULL OR jsonb_typeof(grant_payload)='object'),
    owner_id text NOT NULL CHECK (btrim(owner_id) <> ''),
    admission_id text,
    run_id text,
    task_id text,
    context_digest text,
    state text NOT NULL CHECK (state IN ('CLAIMED','STARTED')),
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    PRIMARY KEY (tenant_id, invocation_id),
    UNIQUE (tenant_id, post_id, persona_id),
    CHECK ((state='STARTED') = (started_at IS NOT NULL))
);
CREATE INDEX persona_invocations_owner ON persona_invocations (tenant_id, owner_id, created_at, invocation_id);
-- +goose StatementBegin
CREATE FUNCTION persona_invocation_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.tenant_id <> OLD.tenant_id OR NEW.invocation_id <> OLD.invocation_id
       OR NEW.conversation_id <> OLD.conversation_id OR NEW.thread_id <> OLD.thread_id
       OR NEW.post_id <> OLD.post_id OR NEW.invoker_id <> OLD.invoker_id
       OR NEW.persona_id <> OLD.persona_id OR NEW.persona_version <> OLD.persona_version
       OR NEW.installation_id <> OLD.installation_id OR NEW.mode <> OLD.mode
       OR NEW.skills <> OLD.skills OR NEW.actor <> OLD.actor OR NEW.owner_id <> OLD.owner_id
       OR NEW.admission_id IS DISTINCT FROM OLD.admission_id OR NEW.run_id IS DISTINCT FROM OLD.run_id
       OR NEW.task_id IS DISTINCT FROM OLD.task_id OR NEW.context_digest IS DISTINCT FROM OLD.context_digest
       OR NEW.created_at <> OLD.created_at THEN
        RAISE EXCEPTION 'persona invocation identity is immutable';
    END IF;
    IF OLD.state = 'STARTED' AND (NEW.state <> OLD.state OR NEW.grant_payload IS DISTINCT FROM OLD.grant_payload) THEN
        RAISE EXCEPTION 'started persona invocation is immutable';
    END IF;
    IF OLD.grant_payload IS NOT NULL AND NEW.grant_payload IS DISTINCT FROM OLD.grant_payload THEN
        RAISE EXCEPTION 'persona invocation grant is immutable';
    END IF;
    IF NEW.state <> OLD.state AND NOT (OLD.state = 'CLAIMED' AND NEW.state = 'STARTED') THEN
        RAISE EXCEPTION 'invalid persona invocation state transition';
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER persona_invocation_guard BEFORE UPDATE ON persona_invocations FOR EACH ROW EXECUTE FUNCTION persona_invocation_guard();
CREATE INDEX persona_invocations_post ON persona_invocations (tenant_id, post_id, persona_id);
ALTER TABLE persona_invocations ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_invocations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_invocations USING (tenant_id = NULLIF(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id',true),'')::uuid);
GRANT SELECT, INSERT, UPDATE ON persona_invocations TO hcmnext_agent_app;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM persona_invocations) THEN
        RAISE EXCEPTION 'cannot remove retained persona invocations';
    END IF;
END $$;
-- +goose StatementEnd
DROP TRIGGER persona_invocation_guard ON persona_invocations;
DROP FUNCTION persona_invocation_guard();
DROP TABLE persona_invocations;
