-- +goose Up
CREATE TABLE chat_saved_item (
    tenant_id text NOT NULL,
    host_tenant_id text NOT NULL,
    home_tenant_id text NOT NULL,
    person_id text NOT NULL,
    conversation_id text NOT NULL,
    post_id text NOT NULL,
    state text NOT NULL DEFAULT 'todo' CHECK (state IN ('todo','done')),
    note text NOT NULL DEFAULT '' CHECK (length(note) <= 4000),
    due_at timestamptz,
    reminded_at timestamptz,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (tenant_id=home_tenant_id),
    PRIMARY KEY (tenant_id,home_tenant_id,person_id,host_tenant_id,conversation_id,post_id)
);
CREATE INDEX chat_saved_page ON chat_saved_item(tenant_id,home_tenant_id,person_id,created_at DESC,host_tenant_id DESC,conversation_id DESC,post_id DESC);
CREATE INDEX chat_saved_due ON chat_saved_item(tenant_id,home_tenant_id,person_id,due_at) WHERE state='todo' AND due_at IS NOT NULL AND reminded_at IS NULL;
ALTER TABLE chat_saved_item ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_saved_item FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_saved_item
    USING (tenant_id=current_setting('hcmnext.tenant_id',true) AND home_tenant_id=current_setting('hcmnext.saved_home_tenant_id',true) AND person_id=current_setting('hcmnext.saved_person_id',true))
    WITH CHECK (tenant_id=current_setting('hcmnext.tenant_id',true) AND home_tenant_id=current_setting('hcmnext.saved_home_tenant_id',true) AND person_id=current_setting('hcmnext.saved_person_id',true));

-- References survive removal of the original post; text is never copied here.
-- +goose StatementBegin
CREATE FUNCTION chat_saved_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='INSERT' THEN
        PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id || chr(31) || NEW.home_tenant_id || chr(31) || NEW.person_id,21));
        IF NOT EXISTS (SELECT 1 FROM chat_saved_item WHERE tenant_id=NEW.tenant_id AND home_tenant_id=NEW.home_tenant_id AND person_id=NEW.person_id AND host_tenant_id=NEW.host_tenant_id AND conversation_id=NEW.conversation_id AND post_id=NEW.post_id)
           AND (SELECT count(*) FROM chat_saved_item WHERE tenant_id=NEW.tenant_id AND home_tenant_id=NEW.home_tenant_id AND person_id=NEW.person_id)>=5000 THEN
            RAISE EXCEPTION 'saved message limit reached' USING ERRCODE='P5021';
        END IF;
    ELSE
        IF (OLD.tenant_id,OLD.home_tenant_id,OLD.person_id,OLD.host_tenant_id,OLD.conversation_id,OLD.post_id,OLD.created_at) IS DISTINCT FROM (NEW.tenant_id,NEW.home_tenant_id,NEW.person_id,NEW.host_tenant_id,NEW.conversation_id,NEW.post_id,NEW.created_at) THEN
            RAISE EXCEPTION 'saved identity is immutable';
        END IF;
        NEW.revision := OLD.revision+1;
        NEW.updated_at := now();
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER chat_saved_guard BEFORE INSERT OR UPDATE ON chat_saved_item FOR EACH ROW EXECUTE FUNCTION chat_saved_guard();

-- Neighbouring chat migrations use the role's existing default privileges.
-- +goose Down
DROP TABLE chat_saved_item;
DROP FUNCTION chat_saved_guard();
