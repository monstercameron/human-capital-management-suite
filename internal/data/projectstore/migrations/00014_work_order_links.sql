-- Project tasks may reference work orders while the work order remains the
-- authoritative source for execution, costs, approvals, and billing.
-- +goose Up
ALTER TABLE project_task_link DROP CONSTRAINT IF EXISTS project_task_link_kind_check;
ALTER TABLE project_task_link DROP CONSTRAINT IF EXISTS project_task_link_check;
ALTER TABLE project_task_link
  ADD CONSTRAINT project_task_link_kind_check CHECK (kind IN ('CHAT_CONVERSATION','CHAT_POST','DEPLOYED_DOCUMENT','WORK_ITEM','JOURNEY','WORK_ORDER')),
  ADD CONSTRAINT project_task_link_check CHECK (
    (kind = 'CHAT_CONVERSATION' AND conversation_id IS NULL AND version_id IS NULL AND scope_id IS NULL) OR
    (kind = 'CHAT_POST' AND conversation_id IS NOT NULL AND version_id IS NULL AND scope_id IS NULL) OR
    (kind = 'DEPLOYED_DOCUMENT' AND conversation_id IS NULL AND version_id IS NOT NULL AND scope_id IS NOT NULL) OR
    (kind IN ('WORK_ITEM','JOURNEY','WORK_ORDER') AND conversation_id IS NULL AND version_id IS NULL AND scope_id IS NULL)
  );

-- +goose Down
-- Refuse to discard work-order references when rolling back this migration.
-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM project_task_link WHERE kind = 'WORK_ORDER') THEN
    RAISE EXCEPTION 'cannot remove WORK_ORDER references while project_task_link rows still use them';
  END IF;
END;
$$;
-- +goose StatementEnd
ALTER TABLE project_task_link DROP CONSTRAINT project_task_link_check;
ALTER TABLE project_task_link DROP CONSTRAINT project_task_link_kind_check;
ALTER TABLE project_task_link
  ADD CONSTRAINT project_task_link_kind_check CHECK (kind IN ('CHAT_CONVERSATION','CHAT_POST','DEPLOYED_DOCUMENT','WORK_ITEM','JOURNEY')),
  ADD CONSTRAINT project_task_link_check CHECK (
    (kind = 'CHAT_CONVERSATION' AND conversation_id IS NULL AND version_id IS NULL AND scope_id IS NULL) OR
    (kind = 'CHAT_POST' AND conversation_id IS NOT NULL AND version_id IS NULL AND scope_id IS NULL) OR
    (kind = 'DEPLOYED_DOCUMENT' AND conversation_id IS NULL AND version_id IS NOT NULL AND scope_id IS NOT NULL) OR
    (kind IN ('WORK_ITEM','JOURNEY') AND conversation_id IS NULL AND version_id IS NULL AND scope_id IS NULL)
  );
