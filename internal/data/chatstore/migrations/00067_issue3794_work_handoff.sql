-- +goose Up
-- Issue 3794: a person offers the next step of one specific work item linked to
-- a conversation to another person, who accepts or declines it. The row is the
-- offer and its answer; the work item itself stays in the project service, so
-- no title, owner or deadline of the work is copied here beyond the owner and
-- deadline the offer was made against (kept to notice a change before accepting).
-- Only the two people named on the row ever read it. A decision changes only
-- status, decided_by and decided_at, and only once.
CREATE TABLE chat_work_handoff (
    tenant_id text NOT NULL,
    id text NOT NULL CHECK (btrim(id) <> ''),
    conversation_id text NOT NULL,
    source_post_id text NOT NULL DEFAULT '',
    project_id text NOT NULL CHECK (btrim(project_id) <> ''),
    task_id text NOT NULL CHECK (btrim(task_id) <> ''),
    mode text NOT NULL CHECK (mode IN ('FOLLOW_UP','TRANSFER')),
    from_id text NOT NULL CHECK (btrim(from_id) <> ''),
    to_id text NOT NULL CHECK (btrim(to_id) <> '' AND to_id <> from_id),
    next_action text NOT NULL CHECK (btrim(next_action) <> '' AND char_length(next_action) <= 200),
    note text NOT NULL DEFAULT '' CHECK (char_length(note) <= 1000),
    owner_at_offer text NOT NULL DEFAULT '',
    due_at_offer text NOT NULL DEFAULT '',
    task_revision bigint NOT NULL DEFAULT 0,
    idempotency_key text NOT NULL CHECK (btrim(idempotency_key) <> ''),
    status text NOT NULL DEFAULT 'OFFERED' CHECK (status IN ('OFFERED','ACCEPTED','DECLINED','WITHDRAWN')),
    offered_at timestamptz NOT NULL DEFAULT now(),
    decided_by text NOT NULL DEFAULT '',
    decided_at timestamptz,
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, from_id, idempotency_key),
    FOREIGN KEY (tenant_id, conversation_id) REFERENCES chat_conversation(tenant_id, id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX chat_work_handoff_one_open ON chat_work_handoff(tenant_id, project_id, task_id, to_id, mode) WHERE status = 'OFFERED';
CREATE INDEX chat_work_handoff_conversation ON chat_work_handoff(tenant_id, conversation_id, offered_at DESC);
ALTER TABLE chat_work_handoff ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_work_handoff FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_work_handoff
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));
-- +goose StatementBegin
CREATE FUNCTION chat_work_handoff_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.status <> 'OFFERED' THEN
        RAISE EXCEPTION 'chat work handoff % is already decided', OLD.id;
    END IF;
    IF NEW.tenant_id <> OLD.tenant_id OR NEW.id <> OLD.id OR NEW.conversation_id <> OLD.conversation_id
       OR NEW.source_post_id <> OLD.source_post_id OR NEW.project_id <> OLD.project_id OR NEW.task_id <> OLD.task_id
       OR NEW.mode <> OLD.mode OR NEW.from_id <> OLD.from_id OR NEW.to_id <> OLD.to_id OR NEW.next_action <> OLD.next_action
       OR NEW.note <> OLD.note OR NEW.owner_at_offer <> OLD.owner_at_offer OR NEW.due_at_offer <> OLD.due_at_offer
       OR NEW.task_revision <> OLD.task_revision OR NEW.idempotency_key <> OLD.idempotency_key OR NEW.offered_at <> OLD.offered_at THEN
        RAISE EXCEPTION 'chat work handoff % keeps what was offered', OLD.id;
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER chat_work_handoff_guard BEFORE UPDATE ON chat_work_handoff FOR EACH ROW EXECUTE FUNCTION chat_work_handoff_guard();

-- The store writes an offer, reads it, and records its answer by update. It
-- never deletes one.
-- A serving role is given what the store runs against the table and nothing
-- more, whether its privileges came from the schema's defaults or from a grant
-- made before this table existed (migration 00041 has the history). Row level
-- security still scopes every statement to the caller's tenant.
-- +goose StatementBegin
DO $$ DECLARE role_name text; BEGIN
    FOR role_name IN SELECT DISTINCT grantee FROM information_schema.role_table_grants
      WHERE table_schema=current_schema() AND table_name='chat_post' AND privilege_type='UPDATE' AND grantee<>'PUBLIC'
    LOOP
      EXECUTE format('GRANT SELECT, INSERT, UPDATE ON chat_work_handoff TO %I',role_name);
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE chat_work_handoff;
DROP FUNCTION chat_work_handoff_guard();
