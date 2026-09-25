-- Retain link-removal evidence and hide tombstoned references from live reads.
-- +goose Up
ALTER TABLE project_task_link
  ADD COLUMN removed_at timestamptz,
  ADD COLUMN removed_by text,
  ADD COLUMN removed_task_revision bigint,
  ADD CONSTRAINT project_task_link_removal_consistent CHECK (
    (removed_at IS NULL AND removed_by IS NULL AND removed_task_revision IS NULL) OR
    (removed_at IS NOT NULL AND removed_by IS NOT NULL AND removed_by <> '' AND removed_task_revision IS NOT NULL AND removed_task_revision > 0)
  );
CREATE INDEX project_task_link_live_page ON project_task_link(tenant_id, project_id, task_id, id) WHERE removed_at IS NULL;

-- Link targets are immutable. Exactly one transition from live to tombstoned
-- is permitted so audit metadata cannot later be rewritten or erased.
-- +goose StatementBegin
CREATE FUNCTION project_task_link_tombstone_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'project task links are retained as tombstones';
  END IF;
  IF OLD.removed_at IS NOT NULL OR NEW.removed_at IS NULL OR
     (to_jsonb(NEW) - ARRAY['removed_at','removed_by','removed_task_revision']) <>
     (to_jsonb(OLD) - ARRAY['removed_at','removed_by','removed_task_revision']) THEN
    RAISE EXCEPTION 'project task link permits only one removal tombstone';
  END IF;
  RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER project_task_link_tombstone_immutable
  BEFORE UPDATE OR DELETE ON project_task_link
  FOR EACH ROW EXECUTE FUNCTION project_task_link_tombstone_guard();

-- +goose Down
DROP TRIGGER project_task_link_tombstone_immutable ON project_task_link;
DROP FUNCTION project_task_link_tombstone_guard();
DROP INDEX project_task_link_live_page;
ALTER TABLE project_task_link DROP CONSTRAINT project_task_link_removal_consistent;
ALTER TABLE project_task_link DROP COLUMN removed_task_revision, DROP COLUMN removed_by, DROP COLUMN removed_at;
