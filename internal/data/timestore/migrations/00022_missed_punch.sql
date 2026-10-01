-- +goose Up
-- TCLOCK-011 persistence: a missed/wrong-punch request and its append-only
-- decision trail. The request row carries the mutable status under a
-- revision guard so two concurrent decisions cannot both win; the decision
-- table never records who requested, only who decided, so the segregation
-- of duties (a supervisor cannot approve their own request) is enforced by
-- the store comparing requested_by against decided_by before it inserts.

CREATE TABLE missed_punch_request (
 tenant_id text NOT NULL, id text NOT NULL,
 worker_ref text NOT NULL, timecard_id text,
 claimed_time timestamptz NOT NULL, reason text NOT NULL CHECK (reason <> ''),
 requested_by text NOT NULL CHECK (requested_by <> ''),
 supervisor_ref text NOT NULL CHECK (supervisor_ref <> ''),
 status text NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING','APPROVED','REJECTED')),
 revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
 period_closed boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id, id)
);
CREATE INDEX missed_punch_request_supervisor ON missed_punch_request(tenant_id, supervisor_ref, status);

CREATE TABLE missed_punch_decision (
 tenant_id text NOT NULL, id text NOT NULL, request_id text NOT NULL,
 decision text NOT NULL CHECK (decision IN ('APPROVED','REJECTED')),
 decided_by text NOT NULL CHECK (decided_by <> ''),
 reason text NOT NULL DEFAULT '',
 reopen_ref text NOT NULL DEFAULT '',
 decided_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id, id),
 FOREIGN KEY (tenant_id, request_id) REFERENCES missed_punch_request(tenant_id, id)
);
CREATE INDEX missed_punch_decision_history ON missed_punch_decision(tenant_id, request_id, decided_at);
CREATE TRIGGER missed_punch_decision_immutable BEFORE UPDATE OR DELETE ON missed_punch_decision
 FOR EACH ROW EXECUTE FUNCTION time_forbid_mutation();

SELECT time_enable_tenant_isolation('missed_punch_request');
SELECT time_enable_tenant_isolation('missed_punch_decision');

-- +goose Down
DROP TABLE missed_punch_decision, missed_punch_request CASCADE;
