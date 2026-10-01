-- +goose Up
-- TCLOCK-011 governed request projection. The legacy missed_punch_request
-- remains readable for compatibility; new workflow requests use this bound
-- projection and carry all immutable evidence needed for review and repair.
CREATE TABLE missed_punch_workflow_request (
 tenant_id text NOT NULL,
 id text NOT NULL,
 session_id text NOT NULL,
 original_observation_id text NOT NULL,
 worker_ref text NOT NULL,
 claimed_out_at timestamptz NOT NULL,
 reason text NOT NULL CHECK (reason <> ''),
 requested_by text NOT NULL CHECK (requested_by <> ''),
 status text NOT NULL CHECK (status IN ('PENDING','APPROVED','REJECTED')),
 request_revision bigint NOT NULL CHECK (request_revision > 0),
 expected_session_revision bigint NOT NULL CHECK (expected_session_revision > 0),
 period_closed boolean NOT NULL,
 original_workflow_instance_ref uuid NOT NULL,
 workflow_instance_id uuid NOT NULL,
 workflow_trace_id text NOT NULL CHECK (workflow_trace_id <> ''),
 workflow_node_id text NOT NULL CHECK (workflow_node_id <> ''),
 workflow_plan_digest text NOT NULL CHECK (workflow_plan_digest <> ''),
 workflow_attempt integer NOT NULL CHECK (workflow_attempt > 0),
 workflow_instance_version bigint NOT NULL CHECK (workflow_instance_version > 0),
 completed_proof_revision bigint NOT NULL DEFAULT 0 CHECK (completed_proof_revision >= 0),
 completed_proof_digest text NOT NULL DEFAULT '',
 completed_proof_instance_id uuid,
 completed_proof_node_id text NOT NULL DEFAULT '',
 completed_proof_attempt integer NOT NULL DEFAULT 0 CHECK (completed_proof_attempt >= 0),
 completed_proof_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id, id),
 FOREIGN KEY (tenant_id, session_id) REFERENCES time_session(tenant_id, id),
 FOREIGN KEY (tenant_id, original_observation_id) REFERENCES time_observation(tenant_id, id)
);
CREATE UNIQUE INDEX missed_punch_workflow_one_pending ON missed_punch_workflow_request(tenant_id, session_id) WHERE status = 'PENDING';
CREATE INDEX missed_punch_workflow_worker ON missed_punch_workflow_request(tenant_id, worker_ref, status);
SELECT time_enable_tenant_isolation('missed_punch_workflow_request');

-- Every workflow result is retained as an immutable proof row. The mutable
-- request projection above is only a current-state index; this table is the
-- reviewable version history and idempotency/input-digest guard.
CREATE TABLE missed_punch_workflow_version (
 tenant_id text NOT NULL,
 id text NOT NULL,
 request_id text NOT NULL,
 revision bigint NOT NULL CHECK (revision > 0),
 action text NOT NULL CHECK (action IN ('REQUEST','APPROVE','REJECT','CORRECTION')),
 status text NOT NULL CHECK (status IN ('PENDING','APPROVED','REJECTED')),
 actor_ref text NOT NULL CHECK (actor_ref <> ''),
 idempotency_key text NOT NULL CHECK (idempotency_key <> ''),
 input_digest text NOT NULL CHECK (input_digest <> ''),
 decision_note text NOT NULL DEFAULT '',
 reopen_ref text NOT NULL DEFAULT '',
 workflow_instance_id uuid NOT NULL,
 workflow_trace_id text NOT NULL CHECK (workflow_trace_id <> ''),
 workflow_node_id text NOT NULL CHECK (workflow_node_id <> ''),
 workflow_plan_digest text NOT NULL CHECK (workflow_plan_digest <> ''),
 workflow_attempt integer NOT NULL CHECK (workflow_attempt > 0),
 workflow_instance_version bigint NOT NULL CHECK (workflow_instance_version > 0),
 correction_observation_id text,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id, id),
 UNIQUE (tenant_id, request_id, revision),
 UNIQUE (tenant_id, request_id, idempotency_key),
 FOREIGN KEY (tenant_id, request_id) REFERENCES missed_punch_workflow_request(tenant_id, id),
 FOREIGN KEY (tenant_id, correction_observation_id) REFERENCES time_observation(tenant_id, id)
);
CREATE INDEX missed_punch_workflow_version_history ON missed_punch_workflow_version(tenant_id, request_id, revision);
CREATE TRIGGER missed_punch_workflow_version_immutable BEFORE UPDATE OR DELETE ON missed_punch_workflow_version
 FOR EACH ROW EXECUTE FUNCTION time_forbid_mutation();
SELECT time_enable_tenant_isolation('missed_punch_workflow_version');

-- Completion evidence is separate from business request revisions. A retry
-- may return the same proof, but an existing proof can never be overwritten.
CREATE TABLE missed_punch_workflow_execution_proof (
 tenant_id text NOT NULL,
 id text NOT NULL,
 request_id text NOT NULL,
 proof_revision bigint NOT NULL CHECK (proof_revision > 0),
 workflow_instance_id uuid NOT NULL,
 workflow_trace_id text NOT NULL CHECK (workflow_trace_id <> ''),
 workflow_node_id text NOT NULL CHECK (workflow_node_id <> ''),
 workflow_plan_digest text NOT NULL CHECK (workflow_plan_digest <> ''),
 workflow_attempt integer NOT NULL CHECK (workflow_attempt > 0),
 workflow_instance_version bigint NOT NULL CHECK (workflow_instance_version > 0),
 proof_digest text NOT NULL CHECK (proof_digest <> ''),
 proof_payload jsonb NOT NULL,
 completed_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id, id),
 UNIQUE (tenant_id, request_id, proof_revision),
 UNIQUE (tenant_id, request_id, workflow_instance_id, workflow_node_id, workflow_attempt),
 FOREIGN KEY (tenant_id, request_id) REFERENCES missed_punch_workflow_request(tenant_id, id)
);
CREATE INDEX missed_punch_workflow_execution_proof_history ON missed_punch_workflow_execution_proof(tenant_id, request_id, proof_revision);
CREATE TRIGGER missed_punch_workflow_execution_proof_immutable BEFORE UPDATE OR DELETE ON missed_punch_workflow_execution_proof
 FOR EACH ROW EXECUTE FUNCTION time_forbid_mutation();
SELECT time_enable_tenant_isolation('missed_punch_workflow_execution_proof');

-- +goose Down
DROP TABLE missed_punch_workflow_execution_proof, missed_punch_workflow_version, missed_punch_workflow_request CASCADE;
