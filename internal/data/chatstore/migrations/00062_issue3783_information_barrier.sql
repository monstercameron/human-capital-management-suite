-- +goose Up
-- Issue 3783: information barriers. One row keeps two groups (teams of the
-- people directory) from sharing a conversation. The pair is stored in key
-- order (group_a < group_b, lower-cased) so the same pair is one rule whichever
-- side an administrator named first; the labels keep the spelling they typed.
-- A rule is never deleted: removing it stamps revoked_at and revoked_by, and
-- both changes are written to the audit log in the same transaction.
CREATE TABLE chat_information_barrier (
    tenant_id text NOT NULL,
    id text NOT NULL CHECK (btrim(id) <> ''),
    group_a text NOT NULL CHECK (btrim(group_a) <> '' AND char_length(group_a) <= 80),
    group_b text NOT NULL CHECK (btrim(group_b) <> '' AND char_length(group_b) <= 80),
    label_a text NOT NULL CHECK (btrim(label_a) <> '' AND char_length(label_a) <= 80),
    label_b text NOT NULL CHECK (btrim(label_b) <> '' AND char_length(label_b) <= 80),
    created_by text NOT NULL CHECK (btrim(created_by) <> ''),
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_by text,
    revoked_at timestamptz,
    PRIMARY KEY (tenant_id, id),
    CHECK (group_a < group_b),
    CHECK ((revoked_at IS NULL) = (revoked_by IS NULL))
);
CREATE UNIQUE INDEX chat_information_barrier_pair ON chat_information_barrier (tenant_id, group_a, group_b) WHERE revoked_at IS NULL;
ALTER TABLE chat_information_barrier ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_information_barrier FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_information_barrier
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));

-- +goose Down
DROP TABLE chat_information_barrier;
