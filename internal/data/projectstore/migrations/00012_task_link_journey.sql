-- Tasks may link an in-progress workflow run (a promotion journey). Like a
-- work item, a journey link stores only its target ID.
-- +goose Up
ALTER TABLE project_task_link DROP CONSTRAINT IF EXISTS project_task_link_kind_check;
ALTER TABLE project_task_link DROP CONSTRAINT IF EXISTS project_task_link_check;
ALTER TABLE project_task_link
  ADD CONSTRAINT project_task_link_kind_check CHECK (kind IN ('CHAT_CONVERSATION','CHAT_POST','DEPLOYED_DOCUMENT','WORK_ITEM','JOURNEY')),
  ADD CONSTRAINT project_task_link_check CHECK (
    (kind = 'CHAT_CONVERSATION' AND conversation_id IS NULL AND version_id IS NULL AND scope_id IS NULL) OR
    (kind = 'CHAT_POST' AND conversation_id IS NOT NULL AND version_id IS NULL AND scope_id IS NULL) OR
    (kind = 'DEPLOYED_DOCUMENT' AND conversation_id IS NULL AND version_id IS NOT NULL AND scope_id IS NOT NULL) OR
    (kind IN ('WORK_ITEM','JOURNEY') AND conversation_id IS NULL AND version_id IS NULL AND scope_id IS NULL)
  );

-- +goose Down
DELETE FROM project_task_link WHERE kind = 'JOURNEY';
ALTER TABLE project_task_link DROP CONSTRAINT project_task_link_check;
ALTER TABLE project_task_link DROP CONSTRAINT project_task_link_kind_check;
ALTER TABLE project_task_link
  ADD CONSTRAINT project_task_link_kind_check CHECK (kind IN ('CHAT_CONVERSATION','CHAT_POST','DEPLOYED_DOCUMENT','WORK_ITEM')),
  ADD CONSTRAINT project_task_link_check CHECK (
    (kind = 'CHAT_CONVERSATION' AND conversation_id IS NULL AND version_id IS NULL AND scope_id IS NULL) OR
    (kind = 'CHAT_POST' AND conversation_id IS NOT NULL AND version_id IS NULL AND scope_id IS NULL) OR
    (kind = 'DEPLOYED_DOCUMENT' AND conversation_id IS NULL AND version_id IS NOT NULL AND scope_id IS NOT NULL) OR
    (kind = 'WORK_ITEM' AND conversation_id IS NULL AND version_id IS NULL AND scope_id IS NULL)
  );
