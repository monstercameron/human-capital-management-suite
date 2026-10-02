-- +goose Up
-- Deleting a message tombstones its post, and the chatrender_remove_views
-- trigger then deletes the post's derived renderings, jobs and reports in the
-- same statement. The trigger runs as the caller. In a cell whose roles are
-- separated the serving role holds only the schema's default table privileges
-- (SELECT, INSERT, UPDATE), so every delete of a message was refused by the
-- database and the page said the service did not answer. Each role that may
-- write posts may remove the derived rows (row level security still scopes
-- them to the caller's tenant).
-- +goose StatementBegin
DO $$ DECLARE role_name text; derived text; BEGIN
    FOR role_name IN SELECT DISTINCT grantee FROM information_schema.role_table_grants
      WHERE table_schema=current_schema() AND table_name='chat_post' AND privilege_type='UPDATE' AND grantee<>'PUBLIC'
    LOOP
      FOREACH derived IN ARRAY ARRAY['chatrender_rendering','chatrender_job','chatrender_report'] LOOP
        EXECUTE format('GRANT DELETE ON %I TO %I',derived,role_name);
      END LOOP;
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
SELECT 1;
