-- +goose Up
-- Unsaving a message deletes its saved reference. Earlier migrations left every
-- serving role with the schema's default table privileges, which in a cell whose
-- roles are separated are SELECT, INSERT and UPDATE only, so a press on an
-- already saved message was refused by the database and answered 503. Each role
-- that reads saved items may remove its own rows (row level security still
-- scopes them to the caller).
-- +goose StatementBegin
DO $$ DECLARE role_name text; BEGIN
    FOR role_name IN SELECT DISTINCT grantee FROM information_schema.role_table_grants
      WHERE table_schema=current_schema() AND table_name='chat_saved_item' AND privilege_type='SELECT' AND grantee<>'PUBLIC'
    LOOP
      EXECUTE format('GRANT DELETE ON chat_saved_item TO %I',role_name);
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
SELECT 1;
