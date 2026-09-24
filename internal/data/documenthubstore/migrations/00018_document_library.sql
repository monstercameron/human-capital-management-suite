-- Personal document library: each person's own folders, folder placements
-- and stars. These rows are organization only. They never grant, widen or
-- reveal access: every read still passes the grant checks, and a placement
-- or star on a document the person can no longer read is simply not shown.
-- A document sits in at most one of a person's folders; deleting a folder
-- removes its placements and never the documents.
-- +goose Up
CREATE TABLE document_folder (
    id text PRIMARY KEY, tenant_id text NOT NULL, owner_id text NOT NULL,
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    CONSTRAINT document_folder_name_check CHECK (char_length(name) BETWEEN 1 AND 80 AND name = btrim(name))
);
CREATE UNIQUE INDEX document_folder_owner_name ON document_folder(tenant_id, owner_id, lower(name));

CREATE TABLE document_folder_item (
    tenant_id text NOT NULL, owner_id text NOT NULL, document_id text NOT NULL,
    folder_id text NOT NULL, filed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, owner_id, document_id),
    FOREIGN KEY (tenant_id, folder_id) REFERENCES document_folder(tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, document_id) REFERENCES document(tenant_id, id) ON DELETE CASCADE
);
CREATE INDEX document_folder_item_folder ON document_folder_item(tenant_id, folder_id);

CREATE TABLE document_star (
    tenant_id text NOT NULL, owner_id text NOT NULL, document_id text NOT NULL,
    starred_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, owner_id, document_id),
    FOREIGN KEY (tenant_id, document_id) REFERENCES document(tenant_id, id) ON DELETE CASCADE
);

-- A person's library starts from the live allows naming them and from the
-- documents they own; both lookups get an index.
CREATE INDEX document_owner ON document(tenant_id, owner_id);
CREATE INDEX document_grant_subject ON document_grant(tenant_id, subject_kind, subject_id, action) WHERE effect = 'allow' AND revoked = false;

-- +goose StatementBegin
DO $$ DECLARE t text; BEGIN
  FOREACH t IN ARRAY ARRAY['document_folder','document_folder_item','document_star'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = current_setting(''hcmnext.tenant_id'', true)) WITH CHECK (tenant_id = current_setting(''hcmnext.tenant_id'', true))', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP INDEX document_grant_subject;
DROP INDEX document_owner;
DROP TABLE document_star, document_folder_item, document_folder CASCADE;
