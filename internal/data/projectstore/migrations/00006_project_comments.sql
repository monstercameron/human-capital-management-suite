-- Task comments keep immutable revisions and a task-scoped activity stream.
-- +goose Up
CREATE TABLE project_task_comment (
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    task_id text NOT NULL,
    id text NOT NULL,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, project_id, task_id, id),
    FOREIGN KEY (tenant_id, project_id, task_id) REFERENCES project_task(tenant_id, project_id, id)
);

CREATE TABLE project_task_comment_revision (
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    task_id text NOT NULL,
    comment_id text NOT NULL,
    revision bigint NOT NULL CHECK (revision > 0),
    source_text text NOT NULL DEFAULT '',
    safe_html text NOT NULL DEFAULT '',
    mention_handles jsonb NOT NULL DEFAULT '[]'::jsonb,
    mentions jsonb NOT NULL DEFAULT '[]'::jsonb,
    tombstone boolean NOT NULL DEFAULT false,
    actor_id text NOT NULL,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, project_id, task_id, comment_id, revision),
    FOREIGN KEY (tenant_id, project_id, task_id, comment_id)
      REFERENCES project_task_comment(tenant_id, project_id, task_id, id),
    CHECK ((tombstone AND source_text = '' AND safe_html = '') OR
           (NOT tombstone AND source_text <> '' AND safe_html <> ''))
);
CREATE INDEX project_task_comment_revision_page
  ON project_task_comment_revision(tenant_id, project_id, task_id, comment_id, revision);

CREATE TABLE project_task_comment_activity (
    sequence bigint GENERATED ALWAYS AS IDENTITY,
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    task_id text NOT NULL,
    comment_id text NOT NULL,
    revision bigint NOT NULL,
    actor_id text NOT NULL,
    kind text NOT NULL CHECK (kind IN ('COMMENT_CREATED','COMMENT_CORRECTED','COMMENT_TOMBSTONED')),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, sequence),
    FOREIGN KEY (tenant_id, project_id, task_id) REFERENCES project_task(tenant_id, project_id, id),
    FOREIGN KEY (tenant_id, project_id, task_id, comment_id, revision)
      REFERENCES project_task_comment_revision(tenant_id, project_id, task_id, comment_id, revision)
);
CREATE INDEX project_task_comment_activity_page
  ON project_task_comment_activity(tenant_id, project_id, task_id, sequence);

CREATE TRIGGER project_task_comment_immutable BEFORE UPDATE OR DELETE ON project_task_comment
  FOR EACH ROW EXECUTE FUNCTION project_forbid_mutation();
CREATE TRIGGER project_task_comment_revision_immutable BEFORE UPDATE OR DELETE ON project_task_comment_revision
  FOR EACH ROW EXECUTE FUNCTION project_forbid_mutation();
CREATE TRIGGER project_task_comment_activity_immutable BEFORE UPDATE OR DELETE ON project_task_comment_activity
  FOR EACH ROW EXECUTE FUNCTION project_forbid_mutation();

-- +goose StatementBegin
DO $$ DECLARE t text; BEGIN
  FOREACH t IN ARRAY ARRAY['project_task_comment','project_task_comment_revision','project_task_comment_activity'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = current_setting(''hcmnext.tenant_id'', true)) WITH CHECK (tenant_id = current_setting(''hcmnext.tenant_id'', true))', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE project_task_comment_activity;
DROP TABLE project_task_comment_revision;
DROP TABLE project_task_comment;
