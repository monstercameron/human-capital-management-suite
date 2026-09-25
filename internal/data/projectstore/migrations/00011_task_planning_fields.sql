-- Planning fields a project manager maintains on a task, plus the subject
-- that created it (the reporter). Legacy rows keep an empty reporter.
-- +goose Up
ALTER TABLE project_task
  ADD COLUMN created_by text NOT NULL DEFAULT '',
  ADD COLUMN labels text[] NOT NULL DEFAULT '{}',
  ADD COLUMN start_date date,
  ADD COLUMN story_points integer NOT NULL DEFAULT 0,
  ADD CONSTRAINT project_task_story_points_range CHECK (story_points >= 0 AND story_points <= 1000),
  ADD CONSTRAINT project_task_labels_bounded CHECK (cardinality(labels) <= 20);

-- +goose Down
ALTER TABLE project_task
  DROP CONSTRAINT project_task_labels_bounded,
  DROP CONSTRAINT project_task_story_points_range,
  DROP COLUMN story_points,
  DROP COLUMN start_date,
  DROP COLUMN labels,
  DROP COLUMN created_by;
