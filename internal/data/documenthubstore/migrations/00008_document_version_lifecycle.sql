-- HUB-009: lifecycle-aware version updates. Content columns stay immutable;
-- only the status may walk candidate -> deployed -> stale, with redeploy
-- returning stale to deployed and withdraw moving stale to retired. The
-- reviewed/submitted vocabulary lives in document_review decisions, not on
-- the version row, so the check narrows to the states deploys actually
-- write. Delete stays forbidden: rollback is a new deployment (HUB-010).
-- +goose Up
ALTER TABLE document_version DROP CONSTRAINT document_version_status_check;
ALTER TABLE document_version ADD CONSTRAINT document_version_status_check
    CHECK (status IN ('candidate','deployed','stale','retired'));
DROP TRIGGER document_version_immutable ON document_version;
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
CREATE TRIGGER document_version_guarded BEFORE UPDATE OR DELETE ON document_version FOR EACH ROW EXECUTE FUNCTION document_version_guarded_update();

-- +goose Down
DROP TRIGGER document_version_guarded ON document_version;
DROP FUNCTION document_version_guarded_update();
ALTER TABLE document_version DROP CONSTRAINT document_version_status_check;
ALTER TABLE document_version ADD CONSTRAINT document_version_status_check
    CHECK (status IN ('candidate','submitted','reviewed','deployed','stale','retired'));
CREATE TRIGGER document_version_immutable BEFORE UPDATE OR DELETE ON document_version FOR EACH ROW EXECUTE FUNCTION document_forbid_mutation();
