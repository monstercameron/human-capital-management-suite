-- +goose Up
-- CHATBUG-087: saving the reading language and the channel's translation setting
-- writes chat_preference and the chatlang tables, and removing a glossary term or
-- correcting a message's language deletes rows. In a cell whose serving role is
-- granted its table privileges after the migrations ran (a blanket grant of
-- SELECT, INSERT and UPDATE on the tables that existed then), the grant loops of
-- migrations 31 and 39 found no role to give DELETE to, so a delete was refused by
-- the database and the page could only say the setting could not be saved. Every
-- role that may update posts is given exactly what these features need, now and
-- whenever the migration runs; granting again what a role already has changes
-- nothing. Row level security still scopes every statement to the caller's tenant.
-- +goose StatementBegin
DO $$ DECLARE role_name text; BEGIN
    FOR role_name IN SELECT DISTINCT grantee FROM information_schema.role_table_grants
      WHERE table_schema=current_schema() AND table_name='chat_post' AND privilege_type='UPDATE' AND grantee<>'PUBLIC'
    LOOP
      EXECUTE format('GRANT SELECT,INSERT,UPDATE ON chat_preference TO %I',role_name);
      EXECUTE format('GRANT SELECT,INSERT,UPDATE,DELETE ON chatlang_setting TO %I',role_name);
      EXECUTE format('GRANT SELECT,INSERT,DELETE ON chatlang_glossary TO %I',role_name);
      EXECUTE format('GRANT SELECT,INSERT ON chatlang_usage TO %I',role_name);
      EXECUTE format('GRANT SELECT,INSERT,UPDATE,DELETE ON chatrender_rendering TO %I',role_name);
      EXECUTE format('GRANT SELECT,INSERT,UPDATE,DELETE ON chatrender_job TO %I',role_name);
      EXECUTE format('GRANT SELECT,INSERT,UPDATE,DELETE ON chatrender_report TO %I',role_name);
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
SELECT 1;
