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

-- +goose Down
DROP TABLE chat_saved_search;
