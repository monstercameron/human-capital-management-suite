-- +goose Up
-- CHATLANG-003 and CHATLANG-006: what an administrator controls about Chat
-- translation, the glossary that shapes it, and one usage line per engine call.
-- None of these tables holds message text. A rendering stays in
-- chatrender_rendering and follows the original's retention exactly; the
-- original in chat_post and chat_post_revision is the only record.

-- One row per workspace (conversation_id is empty) and one per channel that has
-- a setting. A workspace with no row has translation off.
CREATE TABLE chatlang_setting (
    tenant_id text NOT NULL,
    conversation_id text NOT NULL DEFAULT '',
    translation text NOT NULL DEFAULT 'inherit' CHECK (translation IN ('inherit','on','off')),
    external text NOT NULL DEFAULT 'inherit' CHECK (external IN ('inherit','allowed','barred')),
    -- Workspace row only: the reading languages offered (empty offers all), the
    -- monthly limit in millionths (0 is the product default), whether text may
    -- go to an engine outside the deployment, the formality per language, and
    -- the version of the glossary, which every glossary change advances.
    languages jsonb NOT NULL DEFAULT '[]',
    monthly_budget_micros bigint NOT NULL DEFAULT 0 CHECK (monthly_budget_micros >= 0),
    external_allowed boolean NOT NULL DEFAULT true,
    formality jsonb NOT NULL DEFAULT '{}',
    glossary_version bigint NOT NULL DEFAULT 0,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    updated_by text NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, conversation_id)
);

-- A "keep" term is never translated; a "translate" term has a required
-- translation per target language.
CREATE TABLE chatlang_glossary (
    tenant_id text NOT NULL,
    term_id text NOT NULL,
    kind text NOT NULL CHECK (kind IN ('keep','translate')),
    source_term text NOT NULL CHECK (char_length(source_term) BETWEEN 1 AND 120),
    target_language text NOT NULL DEFAULT '',
    target_term text NOT NULL DEFAULT '' CHECK (char_length(target_term) <= 120),
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, term_id),
    CHECK ((kind='keep' AND target_language='' AND target_term='') OR (kind='translate' AND target_language<>'' AND target_term<>''))
);

-- One line per engine call attempt, written in the same step as the call's
-- result. Identifiers, counts and costs only; never request or response text.
-- Append-only: a correction is a later line.
CREATE TABLE chatlang_usage (
    tenant_id text NOT NULL,
    line_id text NOT NULL,
    at timestamptz NOT NULL DEFAULT now(),
    post_id text NOT NULL,
    revision bigint NOT NULL,
    language text NOT NULL,
    provider text NOT NULL,
    model text NOT NULL,
    instruction_digest text NOT NULL DEFAULT '',
    input_tokens bigint NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens bigint NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    cost_micros bigint NOT NULL DEFAULT 0 CHECK (cost_micros >= 0),
    outcome text NOT NULL CHECK (outcome IN ('translated','failed','discarded')),
    PRIMARY KEY (tenant_id, line_id)
);
CREATE INDEX chatlang_usage_month ON chatlang_usage (tenant_id, at);

-- +goose StatementBegin
DO $$ DECLARE t text; BEGIN
    FOREACH t IN ARRAY ARRAY['chatlang_setting','chatlang_glossary','chatlang_usage'] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
        EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id=current_setting(''hcmnext.tenant_id'',true)) WITH CHECK (tenant_id=current_setting(''hcmnext.tenant_id'',true))', t);
    END LOOP;
END $$;
-- +goose StatementEnd
CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON chatlang_usage FOR EACH ROW EXECUTE FUNCTION chat_forbid_mutation();

-- A cell whose serving roles are separated gives a new table to the roles that
-- already read the rendering jobs, and only what each table needs.
-- +goose StatementBegin
DO $$ DECLARE role_name text; BEGIN
    FOR role_name IN SELECT DISTINCT grantee FROM information_schema.role_table_grants
        WHERE table_schema=current_schema() AND table_name='chatrender_job' AND privilege_type='SELECT' AND grantee<>'PUBLIC'
    LOOP
        EXECUTE format('GRANT SELECT,INSERT,UPDATE,DELETE ON chatlang_setting TO %I', role_name);
        EXECUTE format('GRANT SELECT,INSERT,DELETE ON chatlang_glossary TO %I', role_name);
        EXECUTE format('GRANT SELECT,INSERT ON chatlang_usage TO %I', role_name);
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE chatlang_usage;
DROP TABLE chatlang_glossary;
DROP TABLE chatlang_setting;
