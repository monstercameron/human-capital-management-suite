-- AGENTUX-065: a sealed semantic label on every published document version.
-- The label is chosen at publication from the author's bump choice and
-- computed, never typed; the version row's identity (and every digest, seal
-- and citation over it) is unchanged. Existing versions are labelled once
-- here: sequence n in creation order becomes n.0.0. The guarded-update
-- trigger learns the same rule as the Go deploy path: a label may be set
-- once, from empty, and never changed after.
-- +goose Up
ALTER TABLE document_version ADD COLUMN semantic_version text NOT NULL DEFAULT '';
UPDATE document_version v SET semantic_version = numbered.n || '.0.0'
FROM (SELECT id, tenant_id, row_number() OVER (PARTITION BY tenant_id, document_id ORDER BY created_at, id) AS n FROM document_version) numbered
WHERE v.id = numbered.id AND v.tenant_id = numbered.tenant_id;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION document_version_guarded_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'document append-only relation % cannot be changed', TG_TABLE_NAME;
  END IF;
  IF OLD.id IS DISTINCT FROM NEW.id OR OLD.tenant_id IS DISTINCT FROM NEW.tenant_id
     OR OLD.document_id IS DISTINCT FROM NEW.document_id OR OLD.parent_id IS DISTINCT FROM NEW.parent_id
     OR OLD.creator_id IS DISTINCT FROM NEW.creator_id OR OLD.title IS DISTINCT FROM NEW.title
     OR OLD.locale IS DISTINCT FROM NEW.locale OR OLD.classification IS DISTINCT FROM NEW.classification
     OR OLD.normalized_markdown IS DISTINCT FROM NEW.normalized_markdown
     OR OLD.content_hash IS DISTINCT FROM NEW.content_hash
     OR OLD.renderer_profile IS DISTINCT FROM NEW.renderer_profile
     OR OLD.change_note IS DISTINCT FROM NEW.change_note THEN
    RAISE EXCEPTION 'document version content % is immutable', OLD.id;
  END IF;
  IF OLD.semantic_version IS DISTINCT FROM NEW.semantic_version
     AND (OLD.semantic_version <> '' OR NEW.semantic_version = '') THEN
    RAISE EXCEPTION 'document version label % is sealed', OLD.id;
  END IF;
  IF OLD.status = NEW.status THEN RETURN NEW; END IF;
  IF (OLD.status = 'candidate' AND NEW.status = 'deployed')
     OR (OLD.status = 'deployed' AND NEW.status IN ('stale','deployed','retired'))
     OR (OLD.status = 'stale' AND NEW.status IN ('deployed','retired')) THEN
    RETURN NEW;
  END IF;
  RAISE EXCEPTION 'document version status cannot move from % to %', OLD.status, NEW.status;
END $$;
-- +goose StatementEnd

-- +goose Down
ALTER TABLE document_version DROP COLUMN semantic_version;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION document_version_guarded_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'document append-only relation % cannot be changed', TG_TABLE_NAME;
  END IF;
  IF OLD.id IS DISTINCT FROM NEW.id OR OLD.tenant_id IS DISTINCT FROM NEW.tenant_id
     OR OLD.document_id IS DISTINCT FROM NEW.document_id OR OLD.parent_id IS DISTINCT FROM NEW.parent_id
     OR OLD.creator_id IS DISTINCT FROM NEW.creator_id OR OLD.title IS DISTINCT FROM NEW.title
     OR OLD.locale IS DISTINCT FROM NEW.locale OR OLD.classification IS DISTINCT FROM NEW.classification
     OR OLD.normalized_markdown IS DISTINCT FROM NEW.normalized_markdown
     OR OLD.content_hash IS DISTINCT FROM NEW.content_hash
     OR OLD.renderer_profile IS DISTINCT FROM NEW.renderer_profile
     OR OLD.change_note IS DISTINCT FROM NEW.change_note THEN
    RAISE EXCEPTION 'document version content % is immutable', OLD.id;
  END IF;
  IF OLD.status = NEW.status THEN RETURN NEW; END IF;
  IF (OLD.status = 'candidate' AND NEW.status = 'deployed')
     OR (OLD.status = 'deployed' AND NEW.status IN ('stale','deployed','retired'))
     OR (OLD.status = 'stale' AND NEW.status IN ('deployed','retired')) THEN
    RETURN NEW;
  END IF;
  RAISE EXCEPTION 'document version status cannot move from % to %', OLD.status, NEW.status;
END $$;
-- +goose StatementEnd
