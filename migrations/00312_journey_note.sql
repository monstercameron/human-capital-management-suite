-- Journey notes: free-standing, append-only notes on one promotion journey.
--
-- A note is written by anyone the journey detail admits (the proposer, a
-- routed approver, an administrator) at any stage, including after the
-- journey has closed, so that reviewers can hand context from one stage to
-- the next and a later reader can see why things happened. A note never
-- changes the journey: it is not a decision, carries no authority and moves
-- no lifecycle dimension.
--
-- stage is the journey stage the author was looking at when the note was
-- written ("added during finance review"); it is recorded, not derived later,
-- because the stage a journey is in keeps moving.
--
-- The table is append-only: a note is never edited or removed, so what a
-- reviewer read is what stays on the record. SELECT and INSERT are granted;
-- UPDATE and DELETE are refused by the forbid_mutation trigger. One note per
-- (tenant, idempotency key) makes a retried submission converge to one row.

-- +goose Up

CREATE TABLE journey_note (
    tenant_id        tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    note_id          uuid        NOT NULL,
    intent_id        uuid        NOT NULL,
    author_ref       text        NOT NULL,
    body             text        NOT NULL,
    stage            text        NOT NULL,
    idempotency_key  text        NOT NULL,
    created_at       timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, note_id),
    FOREIGN KEY (tenant_id, intent_id) REFERENCES intent_instance (tenant_id, intent_id),
    CONSTRAINT journey_note_one_per_key UNIQUE (tenant_id, idempotency_key),
    CONSTRAINT journey_note_body_present CHECK (length(btrim(body)) > 0),
    CONSTRAINT journey_note_body_bounded CHECK (char_length(body) <= 2000),
    CONSTRAINT journey_note_author_present CHECK (length(btrim(author_ref)) > 0),
    CONSTRAINT journey_note_stage_present CHECK (stage <> ''),
    CONSTRAINT journey_note_key_present CHECK (idempotency_key <> '' AND char_length(idempotency_key) <= 128)
);

CREATE INDEX journey_note_by_intent ON journey_note (tenant_id, intent_id, created_at, note_id);

ALTER TABLE journey_note ENABLE ROW LEVEL SECURITY;
ALTER TABLE journey_note FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON journey_note
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON journey_note TO hcmnext_app;

CREATE TRIGGER journey_note_forbid_mutation
    BEFORE UPDATE OR DELETE ON journey_note
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00312 is irreversible: migrations 00279-00302 already broke the rollback chain, so this migration keeps that true rather than claiming a reversibility no rollback can ever reach'; END $$;
-- +goose StatementEnd
