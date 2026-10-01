-- +goose Up
-- FTIME-006/007 persistence: draft/published crew shifts as a revisioned
-- aggregate row, with every prior version retained append-only (publish,
-- cancel and reassign all append a new history row rather than overwrite
-- the last one).

CREATE TABLE crew_shift (
 tenant_id text NOT NULL, id text NOT NULL,
 worker_ref text NOT NULL, site_ref text NOT NULL, project_ref text NOT NULL,
 revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
 status text NOT NULL CHECK (status IN ('DRAFT','PUBLISHED','CANCELLED')),
 work_start timestamptz NOT NULL, work_end timestamptz NOT NULL,
 payload jsonb NOT NULL DEFAULT '{}'::jsonb,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id, id),
 CHECK (work_end > work_start)
);
CREATE INDEX crew_shift_worker_window ON crew_shift(tenant_id, worker_ref, work_start);

CREATE TABLE crew_shift_history (
 tenant_id text NOT NULL, id text NOT NULL, shift_id text NOT NULL,
 revision bigint NOT NULL CHECK (revision > 0),
 kind text NOT NULL CHECK (kind IN ('DRAFTED','PUBLISHED','CANCELLED','REASSIGNED')),
 approved_by text NOT NULL DEFAULT '', idempotency_key text NOT NULL, payload jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id, id),
 UNIQUE (tenant_id, shift_id, revision),
 FOREIGN KEY (tenant_id, shift_id) REFERENCES crew_shift(tenant_id, id)
);
CREATE INDEX crew_shift_history_by_shift ON crew_shift_history(tenant_id, shift_id, revision);
CREATE TRIGGER crew_shift_history_immutable BEFORE UPDATE OR DELETE ON crew_shift_history
 FOR EACH ROW EXECUTE FUNCTION time_forbid_mutation();

SELECT time_enable_tenant_isolation('crew_shift');
SELECT time_enable_tenant_isolation('crew_shift_history');

-- +goose Down
DROP TABLE crew_shift_history, crew_shift CASCADE;
