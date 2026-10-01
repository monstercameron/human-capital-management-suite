-- +goose Up
-- WTIME-007 persistence: a working-time ledger keyed by the worker
-- aggregation key. The ledger row carries only the current revision so a
-- DECISION can compare its expected revision against it in one read; every
-- interval that has ever been recorded is an append-only slice a domain
-- window computation reads back, never mutated in place.

CREATE TABLE working_time_ledger (
 tenant_id text NOT NULL, aggregation_key text NOT NULL,
 revision bigint NOT NULL DEFAULT 0 CHECK (revision >= 0),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id, aggregation_key)
);

CREATE TABLE working_time_ledger_interval (
 tenant_id text NOT NULL, id text NOT NULL, aggregation_key text NOT NULL,
 ledger_revision bigint NOT NULL CHECK (ledger_revision > 0),
 kind text NOT NULL CHECK (kind IN ('WORKED','REST','SCHOOL_WEEK')),
 interval_start timestamptz NOT NULL, interval_end timestamptz NOT NULL,
 minutes bigint NOT NULL CHECK (minutes >= 0), source_ref text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id, id),
 UNIQUE (tenant_id, aggregation_key, ledger_revision),
 CHECK (interval_end > interval_start)
);
CREATE INDEX working_time_ledger_interval_window ON working_time_ledger_interval(tenant_id, aggregation_key, interval_start);
CREATE TRIGGER working_time_ledger_interval_immutable BEFORE UPDATE OR DELETE ON working_time_ledger_interval
 FOR EACH ROW EXECUTE FUNCTION time_forbid_mutation();

-- Every DECISION that read a ledger revision is recorded so a second
-- decision presenting the same expected revision can be told it conflicts
-- with the first rather than silently both succeeding.
CREATE TABLE working_time_ledger_decision (
 tenant_id text NOT NULL, id text NOT NULL, aggregation_key text NOT NULL,
 expected_revision bigint NOT NULL CHECK (expected_revision >= 0),
 decision_ref text NOT NULL, decided_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id, id),
 UNIQUE (tenant_id, aggregation_key, expected_revision)
);
CREATE TRIGGER working_time_ledger_decision_immutable BEFORE UPDATE OR DELETE ON working_time_ledger_decision
 FOR EACH ROW EXECUTE FUNCTION time_forbid_mutation();

SELECT time_enable_tenant_isolation('working_time_ledger');
SELECT time_enable_tenant_isolation('working_time_ledger_interval');
SELECT time_enable_tenant_isolation('working_time_ledger_decision');

-- +goose Down
DROP TABLE working_time_ledger_decision, working_time_ledger_interval, working_time_ledger CASCADE;
