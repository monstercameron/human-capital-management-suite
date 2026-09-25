-- Comment command receipts make retries durable and atomic with their effects.
-- +goose Up
CREATE TABLE project_task_comment_idempotency (
    tenant_id text NOT NULL,
    actor_id text NOT NULL,
    action text NOT NULL CHECK (action IN ('COMMENT_CREATE','COMMENT_CORRECT','COMMENT_DELETE')),
    client_key text NOT NULL CHECK (client_key <> ''),
    fingerprint text NOT NULL CHECK (fingerprint <> ''),
    result_json jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, actor_id, action, client_key)
);
CREATE INDEX project_task_comment_idempotency_retention
  ON project_task_comment_idempotency(tenant_id, created_at);
ALTER TABLE project_task_comment_idempotency ENABLE ROW LEVEL SECURITY;
ALTER TABLE project_task_comment_idempotency FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON project_task_comment_idempotency
  USING (tenant_id = current_setting('hcmnext.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));
CREATE TRIGGER project_task_comment_idempotency_immutable BEFORE UPDATE OR DELETE ON project_task_comment_idempotency
  FOR EACH ROW EXECUTE FUNCTION project_forbid_mutation();

-- +goose Down
DROP TABLE project_task_comment_idempotency;
