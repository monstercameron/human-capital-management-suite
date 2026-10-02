-- +goose Up
-- The store removes rows from these tables when a person takes something back:
-- a reaction, a pin, a dismissed private answer, a gate answer, a to-do
-- revision, a voice transcript, an agent's channel policy, a moderation role, a
-- public-audience principal, a delivered outbox row, a conversation and its
-- posts. In a cell whose serving roles are separated those roles hold SELECT,
-- INSERT and UPDATE only, so each of these was refused by the database and
-- answered 503 (migration 00028 fixed the saved list alone). Every role that
-- reads one of these tables may delete from it; row level security still scopes
-- the rows to the caller's tenant.
-- +goose StatementBegin
DO $$ DECLARE target text; role_name text; BEGIN
    FOREACH target IN ARRAY ARRAY[
        'chat_reaction','chat_pin','chat_ephemeral_post','chat_gate_answer',
        'chat_channel_todo_revision','chat_voice_transcript','chat_persona_channel_policy',
        'chat_moderation_role','chat_public_audience_principal','chat_outbox',
        'chat_conversation','chat_post'
    ] LOOP
        IF to_regclass(format('%I.%I', current_schema(), target)) IS NULL THEN
            CONTINUE;
        END IF;
        FOR role_name IN SELECT DISTINCT grantee FROM information_schema.role_table_grants
            WHERE table_schema=current_schema() AND table_name=target AND privilege_type='SELECT' AND grantee<>'PUBLIC'
        LOOP
            EXECUTE format('GRANT DELETE ON %I TO %I', target, role_name);
        END LOOP;
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
SELECT 1;
