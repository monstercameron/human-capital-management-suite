-- AGENT-041/AGENT2-022: explicit current grants and durable operation evidence.
-- +goose Up
CREATE TABLE agent_owner_grant (
 tenant_id uuid NOT NULL REFERENCES tenant(tenant_id), grant_id text NOT NULL,
 principal_id text NOT NULL, audience text NOT NULL CHECK(audience IN ('MEMBER','OWNER','OPERATOR')),
 purpose text NOT NULL, capabilities text[] NOT NULL CHECK(cardinality(capabilities)>0),
 task_id text NOT NULL DEFAULT '', not_before timestamptz NOT NULL,
 expires_at timestamptz NOT NULL, revoked_at timestamptz,
 evidence_ref text NOT NULL CHECK(btrim(evidence_ref)<>''),
 PRIMARY KEY(tenant_id,grant_id), CHECK(expires_at>not_before)
);
CREATE TABLE agent_owner_binding (
 tenant_id uuid NOT NULL REFERENCES tenant(tenant_id), task_id text NOT NULL,
 owner_id text NOT NULL, agent_id text NOT NULL, agent_version text NOT NULL,
 installation_id text NOT NULL, PRIMARY KEY(tenant_id,task_id),
 FOREIGN KEY(tenant_id,task_id) REFERENCES agent_task(tenant_id,task_id)
);
CREATE TABLE agent_owner_stop_audit (
 tenant_id uuid NOT NULL REFERENCES tenant(tenant_id), request_id text NOT NULL,
 task_id text NOT NULL, actor_id text NOT NULL, purpose text NOT NULL,
 incident_id text NOT NULL, reason text NOT NULL, expected_revision bigint NOT NULL,
 audit_id text NOT NULL, occurred_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,request_id),
 FOREIGN KEY(tenant_id,task_id) REFERENCES agent_task(tenant_id,task_id)
);
-- +goose StatementBegin
DO $$ DECLARE name text; BEGIN
 FOREACH name IN ARRAY ARRAY['agent_owner_grant','agent_owner_binding','agent_owner_stop_audit'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',name);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',name);
  EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid) WITH CHECK (tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid)',name);
  EXECUTE format('GRANT SELECT,INSERT,UPDATE ON %I TO hcmnext_app',name);
 END LOOP;
END $$;
-- +goose StatementEnd
CREATE TRIGGER agent_owner_stop_audit_immutable BEFORE UPDATE OR DELETE ON agent_owner_stop_audit FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
-- +goose StatementBegin
CREATE FUNCTION agent_operation_grant_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' OR (to_jsonb(NEW)-'revoked_at') IS DISTINCT FROM (to_jsonb(OLD)-'revoked_at') THEN
  RAISE EXCEPTION 'operation grant evidence is immutable' USING ERRCODE='23514';
 END IF;
 IF OLD.revoked_at IS NOT NULL AND NEW.revoked_at IS DISTINCT FROM OLD.revoked_at THEN
  RAISE EXCEPTION 'revoked operation grant cannot be reinstated' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER agent_owner_grant_guard BEFORE UPDATE OR DELETE ON agent_owner_grant FOR EACH ROW EXECUTE FUNCTION agent_operation_grant_guard();
CREATE TRIGGER agent_owner_binding_immutable BEFORE UPDATE OR DELETE ON agent_owner_binding FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE ON agent_owner_stop_audit FROM hcmnext_app;
-- +goose Down
DROP TABLE agent_owner_stop_audit,agent_owner_binding,agent_owner_grant;
DROP FUNCTION agent_operation_grant_guard();
