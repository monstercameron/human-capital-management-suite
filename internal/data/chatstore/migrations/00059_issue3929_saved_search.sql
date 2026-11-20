-- +goose Up
-- Issue 3929: a search a person chose to keep. The row is a private shortcut:
-- it holds only the name the person gave and the query text (words plus
-- filters) as they typed it. It is never a grant and never a snapshot of
-- results; opening one runs an ordinary search that checks the person's access
-- again. Rows are read, changed and deleted only by their owner, they are not
-- written to any export or suggestion list, and they go when the owner deletes
-- them.
CREATE TABLE chat_saved_search (
    tenant_id text NOT NULL,
    id text NOT NULL,
    home_tenant_id text NOT NULL,
    person_id text NOT NULL,
    name text NOT NULL CHECK (btrim(name) <> '' AND char_length(name) <= 80),
    query text NOT NULL CHECK (btrim(query) <> '' AND char_length(query) <= 512),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id)
);
CREATE UNIQUE INDEX chat_saved_search_name ON chat_saved_search (tenant_id, home_tenant_id, person_id, lower(name));
ALTER TABLE chat_saved_search ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_saved_search FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_saved_search
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));

-- A person renames a saved search and deletes it. A serving role holds SELECT,
-- INSERT and UPDATE at most by default, so without DELETE the database refuses
-- the removal.
-- A serving role is given what the store runs against the table and nothing
-- more, whether its privileges came from the schema's defaults or from a grant
-- made before this table existed (migration 00041 has the history). Row level
-- security still scopes every statement to the caller's tenant.
-- +goose StatementBegin
DO $$ DECLARE role_name text; BEGIN
    FOR role_name IN SELECT DISTINCT grantee FROM information_schema.role_table_grants
      WHERE table_schema=current_schema() AND table_name='chat_post' AND privilege_type='UPDATE' AND grantee<>'PUBLIC'
    LOOP
      EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON chat_saved_search TO %I',role_name);
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE chat_saved_search;
