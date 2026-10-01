-- +goose Up
-- time_session is mutable serving state: the current (or last-closed) state
-- of one worker/assignment session. Exactly one OPEN session per tenant,
-- worker and assignment is enforced by the partial unique index below, not
-- by application locking, so a race between two clock-ins resolves with
-- one winner at the database.
CREATE TABLE time_session (
 tenant_id text NOT NULL, id text NOT NULL,
 worker_ref text NOT NULL, assignment_ref text NOT NULL,
 status text NOT NULL, source text NOT NULL, project_ref text NOT NULL DEFAULT '',
 revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
 opened_at timestamptz NOT NULL, closed_at timestamptz,
 payload jsonb NOT NULL DEFAULT '{}'::jsonb,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id, id)
);

-- One active session per worker assignment per tenant, enforced
-- transactionally by the unique index rather than by a read-then-write race.
CREATE UNIQUE INDEX time_session_one_open ON time_session(tenant_id, worker_ref, assignment_ref) WHERE status = 'OPEN';
CREATE INDEX time_session_worker_range ON time_session(tenant_id, worker_ref, opened_at, id);

-- +goose StatementBegin
SELECT time_enable_tenant_isolation('time_session');
-- +goose StatementEnd

-- +goose Down
DROP TABLE time_session CASCADE;
