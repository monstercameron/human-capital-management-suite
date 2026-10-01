-- +goose Up
-- OPEN and ON_BREAK are both active session states. Refuse to install the
-- stronger invariant if legacy data already contains a collision; repairing
-- labor records belongs to an explicit data-migration decision.
DROP INDEX time_session_one_open;
CREATE UNIQUE INDEX time_session_one_open ON time_session(tenant_id, worker_ref, assignment_ref) WHERE status IN ('OPEN','ON_BREAK');

-- +goose Down
-- Do not weaken the active-session invariant while a break session exists.
-- +goose StatementBegin
DO $$
BEGIN
 IF EXISTS (SELECT 1 FROM time_session WHERE status = 'ON_BREAK') THEN
  RAISE EXCEPTION 'cannot remove ON_BREAK active-session protection while break sessions exist';
 END IF;
 DROP INDEX time_session_one_open;
 CREATE UNIQUE INDEX time_session_one_open ON time_session(tenant_id, worker_ref, assignment_ref) WHERE status = 'OPEN';
END $$;
-- +goose StatementEnd
