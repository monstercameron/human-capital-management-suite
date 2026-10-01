-- +goose Up
-- time_session_event is the append-only journal of session transitions: the
-- evidence ApplySessionTransition writes alongside each time_session update,
-- in the same transaction, so a session's current row and its history never
-- disagree. PERMANENT: nothing here is ever updated or deleted.
CREATE TABLE time_session_event (
 tenant_id text NOT NULL, id text NOT NULL, session_id text NOT NULL,
 sequence bigint NOT NULL CHECK (sequence > 0), revision bigint NOT NULL CHECK (revision > 0),
 kind text NOT NULL, actor_ref text NOT NULL,
 idempotency_key text NOT NULL, digest text NOT NULL,
 payload jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id, id),
 UNIQUE (tenant_id, session_id, sequence),
 UNIQUE (tenant_id, session_id, idempotency_key),
 FOREIGN KEY (tenant_id, session_id) REFERENCES time_session(tenant_id, id)
);
CREATE INDEX time_session_event_history ON time_session_event(tenant_id, session_id, sequence);
CREATE TRIGGER time_session_event_immutable BEFORE UPDATE OR DELETE ON time_session_event FOR EACH ROW EXECUTE FUNCTION time_forbid_mutation();

-- +goose StatementBegin
SELECT time_enable_tenant_isolation('time_session_event');
-- +goose StatementEnd

-- +goose Down
DROP TABLE time_session_event CASCADE;
