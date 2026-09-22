-- REV-102-03: seal the permanent tables the review found mutable and drop
-- the stray UPDATE grant.
--
-- legal_rule_pack (00021) is PERMANENT with UPDATE granted to hcmnext_app
-- and no immutability trigger, although its rows are versioned by INSERT
-- (unique on pack_key + version) and internal/governance/legal only ever
-- INSERTs and SELECTs them: this migration seals it with forbid_mutation
-- and drops the UPDATE grant, matching the SELECT/INSERT-only tables beside
-- it. leave_availability_revision (00281) carries a full forbid_mutation
-- trigger and its own header comment calls every revision table
-- SELECT/INSERT-only, yet hcmnext_app holds UPDATE on it: the grant can
-- never succeed, so this migration drops it. record_copy_link (00056) and
-- performance_rating_case (00108) stay mutable by design -- the recordsmeta
-- hold/disposition flow and the performancestore finalization CAS UPDATE
-- them -- and are reclassified to OPERATIONAL in
-- definitions/storage/storage-disposition.yaml instead of being sealed here.

-- +goose Up

REVOKE UPDATE ON legal_rule_pack FROM hcmnext_app;
REVOKE UPDATE ON legal_rule_pack FROM PUBLIC;

CREATE TRIGGER legal_rule_pack_append_only
    BEFORE UPDATE OR DELETE ON legal_rule_pack
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE ON leave_availability_revision FROM hcmnext_app;
REVOKE UPDATE ON leave_availability_revision FROM PUBLIC;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00323 is irreversible: restoring UPDATE on legal_rule_pack would reopen in-place rewrites of versioned legal content, and migrations 00279-00322 already broke the rollback chain, so this migration keeps that true rather than claiming a reversibility no rollback can ever reach'; END $$;
-- +goose StatementEnd
