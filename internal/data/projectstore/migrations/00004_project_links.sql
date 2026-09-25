-- Typed task links retain target identifiers only. Authorized previews are
-- resolved from the target owner at read time and never cached here.
-- +goose Up
ALTER TABLE project_task ADD CONSTRAINT project_task_tenant_project_id_key UNIQUE (tenant_id, project_id, id);

CREATE TABLE project_task_link (
    tenant_id text NOT NULL,
    id text NOT NULL,
    project_id text NOT NULL,
    task_id text NOT NULL,
    kind text NOT NULL CHECK (kind IN ('CHAT_CONVERSATION','CHAT_POST','DEPLOYED_DOCUMENT','WORK_ITEM')),
    target_id text NOT NULL,
    conversation_id text,
    version_id text,
    scope_id text,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    FOREIGN KEY (tenant_id, project_id, task_id) REFERENCES project_task(tenant_id, project_id, id) ON DELETE CASCADE,
    CHECK (
      (kind = 'CHAT_CONVERSATION' AND conversation_id IS NULL AND version_id IS NULL AND scope_id IS NULL) OR
      (kind = 'CHAT_POST' AND conversation_id IS NOT NULL AND version_id IS NULL AND scope_id IS NULL) OR
      (kind = 'DEPLOYED_DOCUMENT' AND conversation_id IS NULL AND version_id IS NOT NULL AND scope_id IS NOT NULL) OR
      (kind = 'WORK_ITEM' AND conversation_id IS NULL AND version_id IS NULL AND scope_id IS NULL)
    ),
    CHECK (kind <> 'DEPLOYED_DOCUMENT' OR scope_id <> '')
);
CREATE INDEX project_task_link_page ON project_task_link(tenant_id, project_id, task_id, id);

ALTER TABLE project_task_link ENABLE ROW LEVEL SECURITY;
ALTER TABLE project_task_link FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON project_task_link
  USING (tenant_id = current_setting('hcmnext.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));

-- +goose Down
DROP TABLE project_task_link;
ALTER TABLE project_task DROP CONSTRAINT project_task_tenant_project_id_key;
