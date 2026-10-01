-- +goose Up
CREATE TABLE time_state_layer (
 tenant_id text NOT NULL, id text NOT NULL, subject_id text NOT NULL,
 base_revision bigint NOT NULL CHECK (base_revision > 0), revision bigint NOT NULL CHECK (revision > 0),
 parent_revision bigint NOT NULL CHECK (parent_revision >= 0), approved boolean NOT NULL,
 actor_ref text NOT NULL, reason text NOT NULL, workflow_instance_id uuid NOT NULL,
 workflow_plan_id text NOT NULL, workflow_plan_digest text NOT NULL, workflow_trace_id text NOT NULL, workflow_node_id text NOT NULL,
 workflow_attempt integer NOT NULL CHECK (workflow_attempt > 0), workflow_version bigint NOT NULL CHECK (workflow_version > 0),
 original_observation_id text NOT NULL, idempotency_key text NOT NULL, digest text NOT NULL,
 patches jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,id), UNIQUE (tenant_id,subject_id,idempotency_key), UNIQUE (tenant_id,subject_id,revision)
 ,FOREIGN KEY (tenant_id,original_observation_id) REFERENCES time_observation(tenant_id,id)
);
CREATE TRIGGER time_state_layer_immutable BEFORE UPDATE OR DELETE ON time_state_layer FOR EACH ROW EXECUTE FUNCTION time_forbid_mutation();
SELECT time_enable_tenant_isolation('time_state_layer');

CREATE TABLE time_state_layer_registry (
 tenant_id text NOT NULL, subject_id text NOT NULL, base_revision bigint NOT NULL,
 current_revision bigint NOT NULL CHECK (current_revision >= 0), current_layer_id text,
 base_payload jsonb NOT NULL DEFAULT '{}'::jsonb,
 updated_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY (tenant_id,subject_id)
);
SELECT time_enable_tenant_isolation('time_state_layer_registry');

CREATE TABLE time_state_base (
 tenant_id text NOT NULL, subject_id text NOT NULL, base_revision bigint NOT NULL CHECK (base_revision > 0),
 base_payload jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,subject_id)
);
CREATE TRIGGER time_state_base_immutable BEFORE UPDATE OR DELETE ON time_state_base FOR EACH ROW EXECUTE FUNCTION time_forbid_mutation();
SELECT time_enable_tenant_isolation('time_state_base');

CREATE TABLE time_state_layer_ledger (
 tenant_id text NOT NULL, id text NOT NULL, subject_id text NOT NULL, revision bigint NOT NULL,
 layer_id text NOT NULL, event_type text NOT NULL, digest text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY (tenant_id,id),
 UNIQUE (tenant_id,subject_id,revision,event_type)
);
CREATE TRIGGER time_state_layer_ledger_immutable BEFORE UPDATE OR DELETE ON time_state_layer_ledger FOR EACH ROW EXECUTE FUNCTION time_forbid_mutation();
SELECT time_enable_tenant_isolation('time_state_layer_ledger');

-- +goose Down
DROP TABLE time_state_layer_ledger, time_state_base, time_state_layer_registry, time_state_layer CASCADE;
