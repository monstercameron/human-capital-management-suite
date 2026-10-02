-- +goose Up
ALTER TABLE chat_post_revision ADD COLUMN source_language text NOT NULL DEFAULT 'und',
 ADD COLUMN language_confidence double precision NOT NULL DEFAULT 0 CHECK (language_confidence BETWEEN 0 AND 1),
 ADD COLUMN language_spans jsonb NOT NULL DEFAULT '[]',
 ADD COLUMN language_corrected boolean NOT NULL DEFAULT false;

-- Language annotations may change; the authored revision remains immutable.
-- +goose StatementBegin
CREATE FUNCTION chatrender_revision_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP = 'DELETE' OR
 (to_jsonb(OLD) - ARRAY['source_language','language_confidence','language_spans','language_corrected']) IS DISTINCT FROM
 (to_jsonb(NEW) - ARRAY['source_language','language_confidence','language_spans','language_corrected']) THEN
  RAISE EXCEPTION 'chat revision is immutable';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
DROP TRIGGER chat_post_revision_immutable ON chat_post_revision;
CREATE TRIGGER chat_post_revision_immutable BEFORE UPDATE OR DELETE ON chat_post_revision FOR EACH ROW EXECUTE FUNCTION chatrender_revision_guard();

CREATE UNIQUE INDEX chatrender_post_tenant_id ON chat_post(tenant_id,id);
CREATE TABLE chatrender_rendering (
 tenant_id text NOT NULL, post_id text NOT NULL,
 revision bigint NOT NULL CHECK (revision > 0), tone text NOT NULL CHECK (tone IN ('as-written','reworded')),
 language text NOT NULL, rendering jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,post_id,revision,tone,language),
 FOREIGN KEY (tenant_id,post_id) REFERENCES chat_post(tenant_id,id) ON DELETE CASCADE
);
CREATE TABLE chatrender_job (
 tenant_id text NOT NULL, post_id text NOT NULL,
 revision bigint NOT NULL CHECK (revision > 0), tone text NOT NULL CHECK (tone IN ('as-written','reworded')),
 language text NOT NULL, request jsonb NOT NULL,
 state text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued','claimed','complete','failed')),
 attempts integer NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 3),
 lease_token text NOT NULL DEFAULT '', lease_until timestamptz,
 requested_at timestamptz NOT NULL DEFAULT now(), failure text NOT NULL DEFAULT '',
 PRIMARY KEY (tenant_id,post_id,revision,tone,language),
 FOREIGN KEY (tenant_id,post_id) REFERENCES chat_post(tenant_id,id) ON DELETE CASCADE
);
CREATE TABLE chatrender_report (
 tenant_id text NOT NULL, report_id text NOT NULL, post_id text NOT NULL,
 revision bigint NOT NULL, tone text NOT NULL, language text NOT NULL,
 reporter_home_tenant_id text NOT NULL, reporter_id text NOT NULL,
 reason text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,report_id),
 FOREIGN KEY (tenant_id,post_id) REFERENCES chat_post(tenant_id,id) ON DELETE CASCADE
);
CREATE TRIGGER forbid_mutation BEFORE UPDATE ON chatrender_rendering FOR EACH ROW EXECUTE FUNCTION chat_forbid_mutation();
CREATE TRIGGER forbid_mutation BEFORE UPDATE ON chatrender_report FOR EACH ROW EXECUTE FUNCTION chat_forbid_mutation();
CREATE INDEX chatrender_job_ready ON chatrender_job(tenant_id,state,requested_at);
-- +goose StatementBegin
DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['chatrender_rendering','chatrender_job','chatrender_report'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',t);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',t);
  EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id=current_setting(''hcmnext.tenant_id'',true)) WITH CHECK (tenant_id=current_setting(''hcmnext.tenant_id'',true))',t);
 END LOOP;
END $$;
-- +goose StatementEnd
-- Derived rows disappear in the same transaction as a removal. Held original
-- revisions remain in their existing ledger; derived text is never that record.
-- +goose StatementBegin
CREATE FUNCTION chatrender_remove_views() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' OR NEW.tombstoned THEN
  DELETE FROM chatrender_rendering WHERE tenant_id=OLD.tenant_id AND post_id=OLD.id;
  DELETE FROM chatrender_job WHERE tenant_id=OLD.tenant_id AND post_id=OLD.id;
  DELETE FROM chatrender_report WHERE tenant_id=OLD.tenant_id AND post_id=OLD.id;
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER chatrender_remove_views BEFORE UPDATE OR DELETE ON chat_post FOR EACH ROW EXECUTE FUNCTION chatrender_remove_views();

-- +goose Down
DROP TRIGGER chatrender_remove_views ON chat_post;
DROP FUNCTION chatrender_remove_views();
DROP TABLE chatrender_report,chatrender_job,chatrender_rendering;
DROP INDEX chatrender_post_tenant_id;
DROP TRIGGER chat_post_revision_immutable ON chat_post_revision;
CREATE TRIGGER chat_post_revision_immutable BEFORE UPDATE OR DELETE ON chat_post_revision FOR EACH ROW EXECUTE FUNCTION chat_forbid_mutation();
DROP FUNCTION chatrender_revision_guard();
ALTER TABLE chat_post_revision DROP COLUMN source_language, DROP COLUMN language_confidence, DROP COLUMN language_spans, DROP COLUMN language_corrected;
