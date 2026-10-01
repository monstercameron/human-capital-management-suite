-- Immutable semantic contracts and current publication authority are owned by
-- the isolated agent data plane. Request-serving credentials only read them.
-- +goose Up
SELECT pg_advisory_xact_lock(hashtext('migration:hcmnext_agent_model_policy_authority'));
-- +goose StatementBegin
DO $$ BEGIN
 CREATE ROLE hcmnext_agent_model_policy_authority NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOLOGIN NOREPLICATION NOBYPASSRLS;
EXCEPTION WHEN duplicate_object OR unique_violation THEN NULL;
END $$;
-- +goose StatementEnd
ALTER ROLE hcmnext_agent_model_policy_authority NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOLOGIN NOREPLICATION NOBYPASSRLS;
-- +goose StatementBegin
DO $$ BEGIN
 EXECUTE format('GRANT USAGE ON SCHEMA %I TO hcmnext_agent_model_policy_authority',current_schema());
END $$;
-- +goose StatementEnd
GRANT SELECT ON tenant TO hcmnext_agent_model_policy_authority;
CREATE TABLE agent_immutable_contract (
 tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
 kind text NOT NULL CHECK (kind IN ('model_policy','output_schema','evaluation_suite')),
 contract_id text NOT NULL CHECK (btrim(contract_id)<>''),
 version bigint NOT NULL CHECK (version>0),
 schema_version integer NOT NULL CHECK (schema_version>0),
 digest text NOT NULL CHECK (digest ~ '^sha256:[0-9a-f]{64}$'),
 content bytea NOT NULL CHECK (octet_length(content)>0 AND octet_length(content)<=1048576),
 content_digest text NOT NULL CHECK (content_digest ~ '^sha256:[0-9a-f]{64}$'),
 recorded_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,kind,contract_id,version,schema_version),
 UNIQUE(tenant_id,kind,contract_id,version,schema_version,digest)
);
CREATE TABLE agent_contract_authority (
 tenant_id uuid NOT NULL,
 kind text NOT NULL,
 contract_id text NOT NULL,
 version bigint NOT NULL,
 schema_version integer NOT NULL,
 digest text NOT NULL,
 revision bigint NOT NULL CHECK(revision>0),
 source_id text NOT NULL CHECK(btrim(source_id)<>''),
 source_revision bigint NOT NULL CHECK(source_revision>0),
 authority jsonb NOT NULL CHECK(jsonb_typeof(authority)='object'),
 revoked boolean NOT NULL,
 effective_from timestamptz NOT NULL,
 effective_until timestamptz NOT NULL CHECK(effective_until>effective_from),
 recorded_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,kind,contract_id,version,schema_version,revision),
 UNIQUE(tenant_id,kind,contract_id,version,schema_version,source_id,source_revision),
 FOREIGN KEY(tenant_id,kind,contract_id,version,schema_version,digest) REFERENCES agent_immutable_contract(tenant_id,kind,contract_id,version,schema_version,digest)
);
CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON agent_immutable_contract FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON agent_contract_authority FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
ALTER TABLE agent_immutable_contract ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_immutable_contract FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_immutable_contract USING(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);
ALTER TABLE agent_contract_authority ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_contract_authority FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_contract_authority USING(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);
REVOKE ALL ON agent_immutable_contract,agent_contract_authority FROM PUBLIC,hcmnext_agent_app;
GRANT SELECT ON agent_immutable_contract,agent_contract_authority TO hcmnext_agent_app;
GRANT SELECT,INSERT ON agent_immutable_contract,agent_contract_authority TO hcmnext_agent_model_policy_authority;
-- +goose Down
DROP TABLE agent_contract_authority;
DROP TABLE agent_immutable_contract;
