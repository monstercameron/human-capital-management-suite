-- Document media: images and PDFs uploaded into a personal document. The
-- bytes are content-addressed (sha256) and live outside the database under
-- the configured media root; each row binds one blob to one document with
-- the media type the server sniffed from the bytes (never the client's
-- declared type), its size and cheap facts (pixel size, PDF page count).
-- Rows are immutable evidence: a re-upload of the same bytes to the same
-- document reuses its row, and access always follows the document's grants.
-- +goose Up
CREATE TABLE document_media (
    id text PRIMARY KEY, tenant_id text NOT NULL, document_id text NOT NULL,
    sha256 text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    filename text NOT NULL, media_type text NOT NULL
        CHECK (media_type IN ('image/png','image/jpeg','image/gif','image/webp','application/pdf')),
    size_bytes bigint NOT NULL CHECK (size_bytes > 0),
    width integer NOT NULL DEFAULT 0, height integer NOT NULL DEFAULT 0, page_count integer NOT NULL DEFAULT 0,
    uploaded_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, document_id, sha256),
    FOREIGN KEY (tenant_id, document_id) REFERENCES document(tenant_id, id) ON DELETE CASCADE
);
CREATE INDEX document_media_document ON document_media(tenant_id, document_id, created_at);

CREATE TRIGGER document_media_immutable BEFORE UPDATE OR DELETE ON document_media FOR EACH ROW EXECUTE FUNCTION document_forbid_mutation();

-- +goose StatementBegin
DO $$ DECLARE t text; BEGIN
  FOREACH t IN ARRAY ARRAY['document_media'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = current_setting(''hcmnext.tenant_id'', true)) WITH CHECK (tenant_id = current_setting(''hcmnext.tenant_id'', true))', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE document_media;
