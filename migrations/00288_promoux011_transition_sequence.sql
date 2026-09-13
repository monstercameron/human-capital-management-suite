-- PROMOUX-011: give every committed promotion transition a durable, globally
-- ordered position before it is ever authority-filtered for a viewer.
--
-- Why this table exists. GREEN requires "one authority-filtered invalidation
-- with sequence" per committed transition, and the live-audit clause this
-- todo turns on is a disclosure bug, not a cosmetic one: if the sequence a
-- viewer observes were the same shared per-tenant counter every subscriber
-- saw, and the authority filter simply dropped the items a viewer may not
-- see, a viewer who observes sequence 1, 2, 4 has learned that transition 3
-- exists even though its content was withheld -- they can count promotions
-- they have no authority over. internal/domains/promotion.SubscriberSequencer
-- closes that leak by handing each subscriber its own gap-free, delivery-time
-- local sequence instead of ever putting this table's value on the wire; what
-- this table is for is a different, narrower guarantee that a per-subscriber
-- in-memory counter cannot provide on its own -- a durable, monotonically
-- increasing position for the underlying committed transition itself,
-- assigned exactly once no matter how many server processes or concurrent
-- goroutines are racing to commit at the same instant. That position becomes
-- each invalidation item's Revision, which is what lets a client that
-- receives messages out of order (internal/transport/productquery's
-- item.Revision <= admissionRevisions[key] check in
-- tools/uxqual/invalidation/processing.go) refuse a stale one instead of
-- silently regressing a region's display state.
--
-- The guarantee is the same shape as migrations/00286 and 00287: a single
-- INSERT .. ON CONFLICT .. DO UPDATE .. RETURNING is the allocation decision,
-- never a SELECT-then-write, so two concurrent transactions racing for the
-- same (tenant, projection) counter serialize on this row's own commit and
-- neither can observe or hand out the other's position.
-- TestTodo_PROMOUX_011_Race proves exactly that against real, concurrent
-- PostgreSQL sessions started from independent connections and released from
-- one barrier, in the same shape TestTodo_PROMOUX_002_Race and
-- TestTodo_PROMOUX_004_Race already established for this schema.

-- +goose Up

CREATE TABLE promotion_invalidation_sequence (
    tenant_id      tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    -- The wire projection name this counter orders (e.g. "promotion_detail",
    -- "promotion_journeys"): each region invalidation
    -- (internal/domains/promotion.Region) advances its own counter so that a
    -- transition affecting only one region never has to skip positions in
    -- another region's sequence space.
    projection     semantic_key NOT NULL,
    next_sequence  bigint      NOT NULL DEFAULT 0,
    updated_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, projection),
    CONSTRAINT promotion_invalidation_sequence_non_negative CHECK (next_sequence >= 0)
);

ALTER TABLE promotion_invalidation_sequence ENABLE ROW LEVEL SECURITY;
ALTER TABLE promotion_invalidation_sequence FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON promotion_invalidation_sequence
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- The counter is live, mutable serving state (an allocator, not a history):
-- UPDATE is granted, DELETE is not, and there is no forbid_mutation trigger,
-- matching promotion_active_intent_guard's (00286) OPERATIONAL shape rather
-- than ledger_event's append-only one.
GRANT SELECT, INSERT, UPDATE ON promotion_invalidation_sequence TO hcmnext_app;

-- +goose Down
-- Every migration from 00279 on declares itself irreversible
-- (migrations/migrations_test.go's
-- TestNewestReversibleVersionStopsBelowDeclaredIrreversibles enforces this as
-- a chain-wide invariant: goose Down runs newest-to-oldest, so a real
-- rollback below the tip must pass back through 00279-00287 in order and
-- would stop at the first of those regardless of what this file's Down
-- section claimed). This migration keeps that chain true rather than
-- asserting a reversibility no rollback can ever actually reach.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00288 is irreversible: migrations 00279-00287 already broke the rollback chain, so this migration keeps that true rather than claiming a reversibility no rollback can ever reach'; END $$;
-- +goose StatementEnd
