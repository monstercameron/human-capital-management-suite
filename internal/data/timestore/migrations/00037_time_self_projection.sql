-- +goose Up
-- Durable worker self-clock projection version. One row exists for every
-- current worker/assignment pair and is advanced in the same transaction as
-- a human punch commit; revision 1 is the initial not-clocked-in snapshot.
CREATE TABLE time_self_clock_projection (
    tenant_id       text NOT NULL,
    worker_ref      text NOT NULL,
    assignment_ref  text NOT NULL,
    revision        bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    status_code     text NOT NULL DEFAULT 'CLOCKED_OUT',
    last_event_at   timestamptz,
    updated_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, worker_ref, assignment_ref)
);

SELECT time_enable_tenant_isolation('time_self_clock_projection');

-- +goose Down
DROP TABLE time_self_clock_projection;
