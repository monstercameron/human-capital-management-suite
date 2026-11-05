-- +goose Up
ALTER TABLE persona_icons
    ADD COLUMN image BYTEA,
    ADD COLUMN image_media_type TEXT CHECK (image_media_type IS NULL OR image_media_type = 'image/jpeg');
ALTER TABLE persona_icon_events DROP CONSTRAINT persona_icon_events_action_check;
ALTER TABLE persona_icon_events
    ADD CONSTRAINT persona_icon_events_action_check CHECK (action IN ('create', 'backfill', 'regenerate', 'shuffle', 'reset', 'undo', 'upload')),
    ADD COLUMN previous_image BYTEA,
    ADD COLUMN image BYTEA;

-- +goose Down
ALTER TABLE persona_icon_events DROP COLUMN image;
ALTER TABLE persona_icon_events DROP COLUMN previous_image;
ALTER TABLE persona_icon_events DROP CONSTRAINT persona_icon_events_action_check;
ALTER TABLE persona_icon_events
    ADD CONSTRAINT persona_icon_events_action_check CHECK (action IN ('create', 'backfill', 'regenerate', 'shuffle', 'reset', 'undo'));
ALTER TABLE persona_icons DROP COLUMN image_media_type;
ALTER TABLE persona_icons DROP COLUMN image;
