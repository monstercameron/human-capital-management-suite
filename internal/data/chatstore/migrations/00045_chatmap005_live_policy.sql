-- +goose Up
-- Live location shares (CHATMAP-005) and their governance (CHATMAP-006).
-- A live share keeps one row and one position: an update replaces the position
-- in place, so no trail of positions exists anywhere in the schema.
ALTER TABLE chat_location_share
 ADD COLUMN live boolean NOT NULL DEFAULT false,
 ADD COLUMN live_interval_seconds integer NOT NULL DEFAULT 0 CHECK (live_interval_seconds >= 0),
 ADD COLUMN position_updated_at timestamptz,
 ADD COLUMN ended_at timestamptz,
 ADD COLUMN ended_reason text NOT NULL DEFAULT '';

-- Every way a share ends (stop, expiry, a removed message, leaving the
-- conversation) goes through UPDATE ... ended=true, so one trigger records when
-- and why without each statement having to.
-- +goose StatementBegin
CREATE FUNCTION chatmap_share_ended() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.ended AND NOT OLD.ended THEN
  IF NEW.ended_at IS NULL THEN
   NEW.ended_at := CASE WHEN NEW.expires_at IS NOT NULL AND NEW.expires_at <= now() THEN NEW.expires_at ELSE now() END;
  END IF;
  IF NEW.ended_reason = '' THEN
   NEW.ended_reason := CASE WHEN NEW.expires_at IS NOT NULL AND NEW.expires_at <= now() THEN 'expired' ELSE 'stopped' END;
  END IF;
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER chatmap_share_ended BEFORE UPDATE ON chat_location_share
 FOR EACH ROW EXECUTE FUNCTION chatmap_share_ended();

-- Leaving a conversation (or being removed from it) ends the person's live
-- shares there. The position is deleted with the statement.
-- +goose StatementBegin
CREATE FUNCTION chatmap_membership_live_end() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.state <> 'active' OR NEW.left_at IS NOT NULL THEN
  UPDATE chat_location_share SET place=NULL,ended=true,ended_reason='left',revision=revision+1
   WHERE tenant_id=NEW.tenant_id AND conversation_id=NEW.conversation_id
    AND sharer_id=NEW.member_id AND sharer_tenant_id=NEW.home_tenant_id AND live AND NOT ended;
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER chatmap_membership_live_end AFTER UPDATE OF state,left_at ON chat_membership
 FOR EACH ROW EXECUTE FUNCTION chatmap_membership_live_end();

-- Administrator settings. conversation_id is empty for the workspace row.
CREATE TABLE chat_location_policy (
 tenant_id text NOT NULL,
 conversation_id text NOT NULL DEFAULT '',
 sharing_enabled boolean NOT NULL DEFAULT true,
 live_enabled boolean NOT NULL DEFAULT true,
 exact_allowed boolean NOT NULL DEFAULT true,
 max_live_seconds integer NOT NULL DEFAULT 28800 CHECK (max_live_seconds BETWEEN 60 AND 86400),
 max_retention_seconds integer NOT NULL DEFAULT 86400 CHECK (max_retention_seconds BETWEEN 60 AND 86400),
 updated_by text NOT NULL DEFAULT '',
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id, conversation_id)
);

-- Whether sharing is on per country or legal entity, and the basis for that.
-- The country '*' row is the workspace-wide rule.
CREATE TABLE chat_location_jurisdiction (
 tenant_id text NOT NULL,
 country text NOT NULL,
 enabled boolean NOT NULL,
 basis text NOT NULL CHECK (basis <> ''),
 updated_by text NOT NULL DEFAULT '',
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id, country)
);

ALTER TABLE chat_location_policy ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_location_policy FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_location_policy
 USING (tenant_id=current_setting('hcmnext.tenant_id',true))
 WITH CHECK (tenant_id=current_setting('hcmnext.tenant_id',true));
ALTER TABLE chat_location_jurisdiction ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_location_jurisdiction FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_location_jurisdiction
 USING (tenant_id=current_setting('hcmnext.tenant_id',true))
 WITH CHECK (tenant_id=current_setting('hcmnext.tenant_id',true));

-- The serving role holds SELECT, INSERT and UPDATE by default. Settings can be
-- removed (a channel returns to the workspace rule) and a share row can be
-- erased with its message, so each role that writes shares also gets DELETE,
-- and the new tables get the same grants (pattern: migrations 00028 and 00039).
-- +goose StatementBegin
DO $$ DECLARE grant_row record; BEGIN
 FOR grant_row IN SELECT grantee,privilege_type FROM information_schema.role_table_grants
  WHERE table_schema=current_schema() AND table_name='chat_location_share' AND privilege_type IN ('SELECT','INSERT','UPDATE','DELETE') AND grantee<>'PUBLIC'
 LOOP
  EXECUTE format('GRANT %s ON chat_location_policy TO %I',grant_row.privilege_type,grant_row.grantee);
  EXECUTE format('GRANT %s ON chat_location_jurisdiction TO %I',grant_row.privilege_type,grant_row.grantee);
 END LOOP;
 FOR grant_row IN SELECT DISTINCT grantee FROM information_schema.role_table_grants
  WHERE table_schema=current_schema() AND table_name='chat_location_share' AND privilege_type='UPDATE' AND grantee<>'PUBLIC'
 LOOP
  EXECUTE format('GRANT DELETE ON chat_location_share TO %I',grant_row.grantee);
  EXECUTE format('GRANT DELETE ON chat_location_policy TO %I',grant_row.grantee);
  EXECUTE format('GRANT DELETE ON chat_location_jurisdiction TO %I',grant_row.grantee);
 END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER chatmap_membership_live_end ON chat_membership;
DROP FUNCTION chatmap_membership_live_end();
DROP TRIGGER chatmap_share_ended ON chat_location_share;
DROP FUNCTION chatmap_share_ended();
DROP TABLE chat_location_jurisdiction;
DROP TABLE chat_location_policy;
ALTER TABLE chat_location_share DROP COLUMN ended_reason, DROP COLUMN ended_at, DROP COLUMN position_updated_at, DROP COLUMN live_interval_seconds, DROP COLUMN live;
