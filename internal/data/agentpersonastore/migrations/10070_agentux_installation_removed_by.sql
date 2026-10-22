-- AGENTUX-037: who removed a placement, and when. Agent setup lists a removed
-- placement as "Removed by <name> on <date>"; the installation row had only a
-- state, so once it was retired nothing said who retired it. A placement that was
-- replaced by a reviewed reinstall, or retired by the server, has no remover.
-- +goose Up
ALTER TABLE persona_installations ADD COLUMN retired_by text NOT NULL DEFAULT '';
ALTER TABLE persona_installations ADD COLUMN retired_at timestamptz;
ALTER TABLE persona_installations ADD CONSTRAINT persona_installations_retired_by_check
    CHECK ((retired_by = '') = (retired_at IS NULL) AND (retired_by = '' OR state = 'RETIRED'));

-- +goose Down
ALTER TABLE persona_installations DROP CONSTRAINT persona_installations_retired_by_check;
ALTER TABLE persona_installations DROP COLUMN retired_at;
ALTER TABLE persona_installations DROP COLUMN retired_by;
