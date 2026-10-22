-- +goose Up
-- CHATLANG-006 and CHATTONE-002: what is measured about translations, what is
-- done with a reader's "this translation is wrong", and a writer's "Send as
-- written" on one message. No table here holds message text: the monitoring
-- rows are counts and durations per language pair per day, the resolution row
-- says who closed a report and how, and the exemption row names a message.

-- One row per workspace, day and language pair. Written in the same step as each
-- translation attempt, so a failing pair shows within the day without anyone
-- reading a message.
CREATE TABLE chatlang_pair_stat (
    tenant_id text NOT NULL,
    day date NOT NULL,
    source_language text NOT NULL,
    target_language text NOT NULL,
    translations bigint NOT NULL DEFAULT 0 CHECK (translations >= 0),
    failures bigint NOT NULL DEFAULT 0 CHECK (failures >= 0),
    low_confidence bigint NOT NULL DEFAULT 0 CHECK (low_confidence >= 0),
    latency_ms_total bigint NOT NULL DEFAULT 0 CHECK (latency_ms_total >= 0),
    latency_ms_max bigint NOT NULL DEFAULT 0 CHECK (latency_ms_max >= 0),
    PRIMARY KEY (tenant_id, day, source_language, target_language)
);

-- A report in chatrender_report is immutable, so what an administrator did with
-- it is a second row: it exists once the report was resolved or dismissed. A
-- report with no row here is open. The note is the administrator's own words.
CREATE TABLE chatlang_report_resolution (
    tenant_id text NOT NULL,
    report_id text NOT NULL,
    state text NOT NULL CHECK (state IN ('resolved','dismissed')),
    note text NOT NULL DEFAULT '' CHECK (char_length(note) <= 1000),
    resolved_by text NOT NULL CHECK (resolved_by <> ''),
    resolved_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, report_id)
);

-- A writer's "Send as written" for one revision of one message, where the
-- channel lets members read messages as written. Append-only: a later edit is a
-- new revision with no row, so it is reworded again.
CREATE TABLE chattone_reword_exemption (
    tenant_id text NOT NULL,
    post_id text NOT NULL,
    revision bigint NOT NULL CHECK (revision > 0),
    person_id text NOT NULL CHECK (person_id <> ''),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, post_id, revision),
    FOREIGN KEY (tenant_id, post_id) REFERENCES chat_post(tenant_id, id) ON DELETE CASCADE
);
CREATE TRIGGER forbid_mutation BEFORE UPDATE ON chattone_reword_exemption FOR EACH ROW EXECUTE FUNCTION chat_forbid_mutation();

-- +goose StatementBegin
DO $$ DECLARE t text; BEGIN
    FOREACH t IN ARRAY ARRAY['chatlang_pair_stat','chatlang_report_resolution','chattone_reword_exemption'] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
        EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id=current_setting(''hcmnext.tenant_id'',true)) WITH CHECK (tenant_id=current_setting(''hcmnext.tenant_id'',true))', t);
    END LOOP;
END $$;
-- +goose StatementEnd

-- A cell whose serving roles are separated gives each new table to the roles
-- that already read the rendering jobs, and only what the table needs: the
-- statistics and the resolution are updated in place, the exemption is only
-- ever added. Nothing here is deleted by the runtime (retention of the
-- statistics is a later, separate job), so DELETE is granted on none of them.
-- +goose StatementBegin
DO $$ DECLARE role_name text; BEGIN
    FOR role_name IN SELECT DISTINCT grantee FROM information_schema.role_table_grants
        WHERE table_schema=current_schema() AND table_name='chatrender_job' AND privilege_type='SELECT' AND grantee<>'PUBLIC'
    LOOP
        EXECUTE format('GRANT SELECT,INSERT,UPDATE ON chatlang_pair_stat TO %I', role_name);
        EXECUTE format('GRANT SELECT,INSERT,UPDATE ON chatlang_report_resolution TO %I', role_name);
        EXECUTE format('GRANT SELECT,INSERT ON chattone_reword_exemption TO %I', role_name);
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE chattone_reword_exemption;
DROP TABLE chatlang_report_resolution;
DROP TABLE chatlang_pair_stat;
