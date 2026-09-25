-- Reverse lookup: which live tasks link a given workflow run or work item.
-- +goose Up
CREATE INDEX project_task_link_target ON project_task_link(tenant_id, kind, target_id) WHERE removed_at IS NULL;

-- +goose Down
DROP INDEX project_task_link_target;
