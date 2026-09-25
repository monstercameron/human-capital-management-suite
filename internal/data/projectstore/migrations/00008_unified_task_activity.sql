-- Give every task event one durable sequence shared by task mutations and comments.
-- +goose Up
ALTER TABLE project_task ADD COLUMN activity_sequence bigint NOT NULL DEFAULT 0 CHECK (activity_sequence >= 0);
ALTER TABLE project_activity ADD COLUMN task_sequence bigint NOT NULL DEFAULT 0 CHECK (task_sequence >= 0);
ALTER TABLE project_task_comment_activity ADD COLUMN task_sequence bigint NOT NULL DEFAULT 0 CHECK (task_sequence >= 0);

-- Existing append-only records stay untouched. Their sequence is ranked per
-- task at read time; writers lazily seed the counter from visible legacy rows.
CREATE UNIQUE INDEX project_activity_task_sequence ON project_activity(tenant_id, project_id, aggregate_id, task_sequence) WHERE task_sequence > 0;
CREATE UNIQUE INDEX project_task_comment_activity_task_sequence ON project_task_comment_activity(tenant_id, project_id, task_id, task_sequence) WHERE task_sequence > 0;

-- +goose Down
DROP INDEX project_task_comment_activity_task_sequence;
DROP INDEX project_activity_task_sequence;
ALTER TABLE project_task_comment_activity DROP COLUMN task_sequence;
ALTER TABLE project_activity DROP COLUMN task_sequence;
ALTER TABLE project_task DROP COLUMN activity_sequence;
