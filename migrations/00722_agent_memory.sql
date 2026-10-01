-- AGENT-040: durable owner policy and inventory for governed agent copies.
-- +goose Up
CREATE TABLE agent_memory_copy (
 tenant_id uuid NOT NULL REFERENCES tenant(tenant_id), store_name text NOT NULL,
 item_id text NOT NULL, source_owner text NOT NULL, source_id text NOT NULL,
 item jsonb NOT NULL CHECK (jsonb_typeof(item) = 'object'),
 PRIMARY KEY (tenant_id, store_name, item_id)
);
CREATE TABLE agent_memory_invalidation (
 tenant_id uuid NOT NULL REFERENCES tenant(tenant_id), target_kind text NOT NULL CHECK (target_kind IN ('SOURCE','ITEM')),
 target_id text NOT NULL, receipt jsonb NOT NULL,
 PRIMARY KEY (tenant_id, target_kind, target_id)
);
CREATE TABLE agent_memory_policy (
 tenant_id uuid NOT NULL REFERENCES tenant(tenant_id), owner_id text NOT NULL, purpose text NOT NULL,
 policy jsonb NOT NULL, expires_at timestamptz NOT NULL,
 PRIMARY KEY (tenant_id, owner_id, purpose)
);
CREATE TABLE agent_memory_source (
 tenant_id uuid NOT NULL REFERENCES tenant(tenant_id), source_owner text NOT NULL, source_id text NOT NULL,
 purpose text NOT NULL, decision jsonb NOT NULL, expires_at timestamptz NOT NULL,
 PRIMARY KEY (tenant_id, source_owner, source_id, purpose)
);
CREATE TABLE agent_memory_disposition (
 tenant_id uuid NOT NULL REFERENCES tenant(tenant_id), item_id text NOT NULL,
 decision jsonb NOT NULL, expires_at timestamptz NOT NULL,
 PRIMARY KEY (tenant_id, item_id)
);
CREATE TABLE agent_memory_grant (
 tenant_id uuid NOT NULL REFERENCES tenant(tenant_id), grant_id text NOT NULL,
 principal_id text NOT NULL, owner_id text NOT NULL, purpose text NOT NULL,
 operations text[] NOT NULL CHECK (cardinality(operations) > 0),
 not_before timestamptz NOT NULL, expires_at timestamptz NOT NULL,
 evidence_ref text NOT NULL CHECK (btrim(evidence_ref) <> ''), revoked_at timestamptz,
 PRIMARY KEY (tenant_id, grant_id), CHECK (expires_at > not_before)
);
-- +goose StatementBegin
DO $$ DECLARE name text; BEGIN
 FOREACH name IN ARRAY ARRAY['agent_memory_copy','agent_memory_invalidation','agent_memory_policy','agent_memory_source','agent_memory_disposition','agent_memory_grant'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', name);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', name);
  EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = NULLIF(current_setting(''app.tenant_id'', true), '''')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting(''app.tenant_id'', true), '''')::uuid)', name);
  EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON %I TO hcmnext_app', name);
 END LOOP;
END $$;
-- +goose StatementEnd
CREATE TRIGGER agent_memory_invalidation_immutable BEFORE UPDATE OR DELETE ON agent_memory_invalidation FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON agent_memory_invalidation FROM hcmnext_app;
CREATE TRIGGER agent_memory_grant_guard BEFORE UPDATE OR DELETE ON agent_memory_grant FOR EACH ROW EXECUTE FUNCTION agent_operation_grant_guard();
-- +goose Down
DROP TABLE agent_memory_grant, agent_memory_disposition, agent_memory_source, agent_memory_policy, agent_memory_invalidation, agent_memory_copy;
