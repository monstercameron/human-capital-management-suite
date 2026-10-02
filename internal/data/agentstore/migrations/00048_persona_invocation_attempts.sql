-- CHATBUG-047: asking a question again admits the question that already stands
-- as another attempt, instead of posting a copy of it. Each attempt is its own
-- invocation row, named by an identifier derived from the post, the persona and
-- the attempt number, so the primary key already keeps one row per attempt and
-- the (post, persona) uniqueness of the first design is dropped.
-- +goose Up
-- +goose StatementBegin
DO $$
DECLARE
    name text;
BEGIN
    FOR name IN
        SELECT c.conname FROM pg_constraint c
        WHERE c.conrelid = 'persona_invocations'::regclass AND c.contype = 'u'
          AND (SELECT array_agg(a.attname::text ORDER BY a.attname::text) FROM pg_attribute a
               WHERE a.attrelid = c.conrelid AND a.attnum = ANY (c.conkey)) = ARRAY['persona_id', 'post_id', 'tenant_id']
    LOOP
        EXECUTE format('ALTER TABLE persona_invocations DROP CONSTRAINT %I', name);
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM persona_invocations GROUP BY tenant_id, post_id, persona_id HAVING count(*) > 1) THEN
        RAISE EXCEPTION 'persona invocations hold more than one attempt for a post; refusing to restore the one-row rule';
    END IF;
    ALTER TABLE persona_invocations ADD CONSTRAINT persona_invocations_tenant_id_post_id_persona_id_key UNIQUE (tenant_id, post_id, persona_id);
END $$;
-- +goose StatementEnd
