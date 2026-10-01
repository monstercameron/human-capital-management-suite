-- +goose Up
-- FTIME-004/WF-CAP-007 persistence: the timecard aggregate row (revision +
-- state) plus one append-only event table carrying every line, correction,
-- attestation and approval-history entry. The event kind discriminates the
-- entry; the aggregate row always mirrors the latest event's revision.

CREATE TABLE timecard (
 tenant_id text NOT NULL, id text NOT NULL,
 worker_ref text NOT NULL, period_start timestamptz NOT NULL, period_end timestamptz NOT NULL,
 revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
 state text NOT NULL CHECK (state IN ('OPEN','SUBMITTED','APPROVED','REJECTED','REOPENED')),
 payload jsonb NOT NULL DEFAULT '{}'::jsonb,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id, id),
 CHECK (period_end > period_start)
);
CREATE INDEX timecard_worker_period ON timecard(tenant_id, worker_ref, period_start);

CREATE TABLE timecard_event (
 tenant_id text NOT NULL, id text NOT NULL, timecard_id text NOT NULL,
 sequence bigint NOT NULL CHECK (sequence > 0), revision bigint NOT NULL CHECK (revision > 0),
 kind text NOT NULL CHECK (kind IN ('LINE','CORRECTION','ATTESTATION','APPROVAL','REJECTION','REOPEN')),
 actor_id text NOT NULL, idempotency_key text NOT NULL, payload jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id, id),
 UNIQUE (tenant_id, timecard_id, sequence),
 UNIQUE (tenant_id, timecard_id, kind, actor_id, idempotency_key),
 FOREIGN KEY (tenant_id, timecard_id) REFERENCES timecard(tenant_id, id)
);
CREATE INDEX timecard_event_history ON timecard_event(tenant_id, timecard_id, sequence);
CREATE TRIGGER timecard_event_immutable BEFORE UPDATE OR DELETE ON timecard_event
 FOR EACH ROW EXECUTE FUNCTION time_forbid_mutation();

SELECT time_enable_tenant_isolation('timecard');
SELECT time_enable_tenant_isolation('timecard_event');

-- +goose Down
DROP TABLE timecard_event, timecard CASCADE;
