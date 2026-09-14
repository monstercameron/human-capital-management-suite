package lineageconformance_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/lineage"
	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projection"
	"github.com/monstercameron/human-capital-management-suite/internal/data/provenance"
	"github.com/monstercameron/human-capital-management-suite/internal/data/rebuild"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/correction"
	lc "github.com/monstercameron/human-capital-management-suite/tools/planning/lineageconformance"
)

const (
	effectSchemaRef = "hcmnext.people.v1.PromotionEffect@1"
	peopleAuthority = "hcmnext:people"
)

var storeOccurred = time.Date(2026, time.February, 1, 9, 0, 0, 0, time.UTC)

// promotionStore is one migrated schema holding a Promotion's owner
// records: the promotion-outcome ledger stream, its outbox effect, its
// provenance edges and its append-only corrections.
type promotionStore struct {
	db        *pgtest.DB
	tenant    uuid.UUID
	stream    string
	intentRef string
	outboxID  uuid.UUID
}

func newPromotionStore(t *testing.T) *promotionStore {
	t.Helper()
	db := pgtest.New(t)
	db.Exec(t, provenance.SchemaDDL)
	tenant := uuid.New()
	s := &promotionStore{db: db, tenant: tenant, stream: "workflow:promotion:" + tenant.String(), intentRef: "intent/promo-" + tenant.String()}
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'Lineage', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, tenant, "lineage-"+tenant.String())
	db.Exec(t, `INSERT INTO payload_schema (tenant_id, schema_ref, schema_id, schema_version, message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.workflow.PromotionOutcome', 2, 'hcmnext.workflow.PromotionOutcome', 'PROTOBUF', 'LEDGER_EVENT')`, tenant, projection.PromotionOutcomeSchemaRef)
	db.Exec(t, `INSERT INTO payload_schema (tenant_id, schema_ref, schema_id, schema_version, message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'google.protobuf.Struct', 1, 'google.protobuf.Struct', 'PROTOBUF', 'EVIDENCE_MANIFEST')`, tenant, provenance.OutboxSchemaRef)
	db.Exec(t, `INSERT INTO payload_schema (tenant_id, schema_ref, schema_id, schema_version, message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.people.v1.PromotionEffect', 1, 'hcmnext.people.v1.PromotionEffect', 'PROTOBUF', 'LEDGER_EVENT')`, tenant, effectSchemaRef)
	db.Exec(t, `INSERT INTO authority_assignment (tenant_id, authority_ref, authority_kind, domain_scope, effective_from)
		VALUES ($1, $2, 'INTERNAL', 'people', timestamptz '2026-01-01T00:00:00Z')`, tenant, peopleAuthority)
	s.inTx(t, func(tx dbport.Tx) error {
		return ledger.EnsureStream(context.Background(), tx, tenant, s.stream, "TRANSACTION", s.intentRef)
	})
	return s
}

func (s *promotionStore) inTx(t *testing.T, fn func(dbport.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := s.db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func digestOf(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// record writes the Promotion's owner records: the outcome event with its
// outbox effect caused by it, ledger-event and external-observation
// provenance, and one append-only correction with its own provenance.
func (s *promotionStore) record(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	s.inTx(t, func(tx dbport.Tx) error {
		receipt, err := ledger.Append(ctx, tx, ledger.AppendRequest{
			Tenant: s.tenant, StreamKey: s.stream, ExpectedHead: 0, AssertionClass: ledger.TransactionFact,
			SourceRef: "hcmnext:workflow:promotion", SchemaRef: projection.PromotionOutcomeSchemaRef,
			Payload: []byte("promotion:manager"), OccurredAt: storeOccurred, EffectiveAt: storeOccurred,
			CorrelationID: uuid.New(), IdempotencyKey: "promotion-outcome-" + s.tenant.String(),
		})
		if err != nil {
			return err
		}
		ob, err := outbox.Enqueue(ctx, tx, outbox.EnqueueRequest{
			Tenant: s.tenant, EffectIdentity: "promotion.effect:" + receipt.EventID.String(), OrderingKey: s.stream,
			SchemaRef: effectSchemaRef, Payload: []byte("assignment.position_ref=pos-9"),
			Causal: &outbox.CausalMetadata{CorrelationID: s.intentRef, CausationID: receipt.EventID.String(),
				LogicalOperationID: "promotion-effect", AttemptID: "attempt-1"},
		})
		if err != nil {
			return err
		}
		s.outboxID = ob.OutboxID
		if _, err := provenance.Publish(ctx, tx, s.eventProvenance(receipt.Sequence, receipt.EventID, receipt.Digest)); err != nil {
			return err
		}
		_, err = provenance.Publish(ctx, tx, provenance.PublishRequest{
			Tenant: s.tenant, IntentRef: s.intentRef, SourceKind: provenance.SourceExternalObservation,
			SourceRef: "observation/obs-118", ObservationRef: "obs-118", ConnectorRef: "conn/hris",
			SourceAuthority: "connector/hris", PrincipalRef: "op-118", EvidenceIDs: []string{"ev-observation"},
			Digests:     []provenance.Digest{{Kind: "observation", Algorithm: "sha256", Digest: digestOf([]byte("position_ref=pos-9"))}},
			PublishedAt: storeOccurred.Add(2 * time.Hour),
		})
		return err
	})
	s.correct(t, 1, "promotion effective date recorded a day late")
}

func (s *promotionStore) eventProvenance(seq int64, eventID uuid.UUID, digest string) provenance.PublishRequest {
	return provenance.PublishRequest{
		Tenant: s.tenant, IntentRef: s.intentRef, SourceKind: provenance.SourceLedgerEvent,
		SourceRef: provenance.LedgerEventSourceRef(s.stream, seq), StreamKey: s.stream, Sequence: seq, EventID: eventID,
		SourceAuthority: peopleAuthority, PrincipalRef: "workflow-3", EvidenceIDs: []string{"ev-event-" + strconv.FormatInt(seq, 10)},
		Digests:     []provenance.Digest{{Kind: "ledger_event", Algorithm: "sha256", Digest: digest}},
		PublishedAt: storeOccurred.Add(time.Duration(seq) * time.Hour),
	}
}

// correct appends one correction of sequence 1 at the current head and
// publishes its provenance.
func (s *promotionStore) correct(t *testing.T, head int64, reason string) {
	t.Helper()
	ctx := context.Background()
	s.inTx(t, func(tx dbport.Tx) error {
		res, err := correction.Append(ctx, tx, correction.Request{
			Tenant: s.tenant, StreamKey: s.stream, ExpectedHead: head,
			Target:    ledger.EventRef{StreamKey: s.stream, Sequence: 1},
			Authority: peopleAuthority, SourceRef: "hcmnext:workflow:promotion-correction",
			SchemaRef: projection.PromotionOutcomeSchemaRef, Payload: []byte(reason),
			OccurredAt: storeOccurred, EffectiveAt: storeOccurred, CorrelationID: uuid.New(),
			IdempotencyKey: fmt.Sprintf("correction-%d-%s", head, s.tenant), Reason: reason, CorrectedBy: "steward:people",
		}, func() time.Time { return storeOccurred.Add(time.Duration(head+24) * time.Hour) })
		if err != nil {
			return err
		}
		_, err = provenance.Publish(ctx, tx, s.eventProvenance(res.Correction.Sequence, res.Correction.EventID, res.Correction.Digest))
		return err
	})
}

// storeFacts is what the Promotion's owner stores say, read back through
// their own packages, and the lineage graph mapped from them.
type storeFacts struct {
	graph      lc.Graph
	events     []ledger.EventRecord
	head1      projection.PromotionOutcomeReport
	replay     projection.PromotionOutcomeReport
	provenance provenance.Result
}

// readStoreGraph maps owner records onto the Promotion case. The DATA-015
// trace supplies proposal, workflow, transaction, effect and repair; the
// stores supply intent, event, projection, outbox, observation and every
// correction. No store holds a Promotion reconciliation result, so none is
// invented.
func (s *promotionStore) readStoreGraph(t *testing.T, q dbport.Querier, trace lineage.Trace) storeFacts {
	t.Helper()
	ctx := context.Background()
	reader := ledger.NewReader()
	events, err := reader.ReadStream(ctx, q, s.tenant, s.stream)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := rebuild.NewPromotionOutcomeRebuilder(reader).Replay(ctx, q, s.tenant, s.stream)
	if err != nil {
		t.Fatal(err)
	}
	head1, err := projection.RebuildPromotionOutcome(events[:1], s.tenant, s.stream)
	if err != nil {
		t.Fatal(err)
	}
	prov, err := provenance.Lineage(ctx, q, reader, s.tenant, s.stream, s.intentRef)
	if err != nil {
		t.Fatal(err)
	}
	ob, err := outbox.Read(ctx, q, s.tenant, s.outboxID)
	if err != nil {
		t.Fatal(err)
	}

	caseID, tenant := lc.PromotionDefinition, s.tenant.String()
	traced, err := lc.FromTrace(caseID, tenant, trace)
	if err != nil {
		t.Fatal(err)
	}
	var recs []lc.Record
	for _, rec := range traced {
		if rec.Link != lc.LinkEvent && rec.Link != lc.LinkObservation {
			recs = append(recs, rec)
		}
	}
	id := func(l lc.Link) string { return caseID + "#" + string(l) }
	eventMark := provenance.LedgerEventSourceRef(s.stream, events[0].Sequence)
	recs = append(recs,
		lc.Record{ID: id(lc.LinkIntent), Tenant: tenant, Case: caseID, Link: lc.LinkIntent, Watermark: "ledger_stream:" + s.stream, Payload: s.intentRef},
		lc.Record{ID: id(lc.LinkEvent), Tenant: tenant, Case: caseID, Link: lc.LinkEvent, Watermark: eventMark,
			SourceDigest: events[0].Digest, Payload: string(events[0].Payload)},
		lc.Record{ID: id(lc.LinkProjection), Tenant: tenant, Case: caseID, Link: lc.LinkProjection,
			Watermark:    projection.PromotionOutcomeProjectionName + "@" + strconv.FormatInt(head1.SourceHead, 10),
			SourceDigest: head1.Digest, DerivedFrom: []string{id(lc.LinkEvent)}, SourceHead: eventMark,
			Payload: fmt.Sprintf("rows=%d", head1.RowCount)},
	)
	if ob.Causal != nil && ob.Causal.CausationID == events[0].EventID.String() {
		recs = append(recs, lc.Record{ID: id(lc.LinkOutbox), Tenant: tenant, Case: caseID, Link: lc.LinkOutbox,
			Watermark: "outbox:" + ob.OutboxID.String(), SourceDigest: digestOf(ob.Payload), Payload: ob.EffectIdentity})
	}
	for _, edge := range prov.Edges {
		if edge.SourceKind == provenance.SourceExternalObservation {
			recs = append(recs, lc.Record{ID: id(lc.LinkObservation), Tenant: tenant, Case: caseID, Link: lc.LinkObservation,
				Watermark: edge.SourceRef, SourceDigest: edge.Digests[0].Digest, Payload: edge.ObservationRef})
		}
	}
	for _, ev := range events[1:] {
		if ev.AssertionClass != ledger.Correction {
			continue
		}
		var targetStream string
		var targetSeq int64
		if err := q.QueryRow(ctx, `SELECT corrects_stream_key, corrects_sequence FROM ledger_event WHERE tenant_id = $1 AND stream_key = $2 AND sequence = $3`,
			s.tenant, s.stream, ev.Sequence).Scan(&targetStream, &targetSeq); err != nil {
			t.Fatal(err)
		}
		target := id(lc.LinkEvent)
		if targetStream != s.stream || targetSeq != events[0].Sequence {
			target = fmt.Sprintf("%s@%d", id(lc.LinkCorrection), targetSeq)
		}
		recs = append(recs, lc.Record{ID: fmt.Sprintf("%s@%d", id(lc.LinkCorrection), ev.Sequence), Tenant: tenant, Case: caseID,
			Link: lc.LinkCorrection, Watermark: provenance.LedgerEventSourceRef(s.stream, ev.Sequence),
			SourceDigest: ev.Digest, Corrects: target, Payload: string(ev.Payload)})
	}
	return storeFacts{
		graph:  lc.Graph{Tenant: tenant, Records: lc.Seal(lc.Chain("", recs))},
		events: events, head1: head1, replay: replay, provenance: prov,
	}
}

// TestTodo_DATA_022_Integration proves Promotion lineage against the real
// owner stores in embedded PostgreSQL: event, projection rebuild, outbox
// effect caused by the event, provenance and append-only correction are
// read back and pass the shared assertions, and the one link no store
// holds -- RECONCILIATION -- is reported UNKNOWN, so the family is PARTIAL
// rather than falsely complete.
func TestTodo_DATA_022_Integration(t *testing.T) {
	s := newPromotionStore(t)
	s.record(t)
	facts := s.readStoreGraph(t, s.db.Conn, promotionTrace(t, s.tenant.String()))

	if len(facts.events) != 2 || facts.events[1].AssertionClass != ledger.Correction {
		t.Fatalf("stream holds %d events, want the outcome and its correction", len(facts.events))
	}
	live, err := projection.RebuildPromotionOutcome(facts.events, s.tenant, s.stream)
	if err != nil {
		t.Fatal(err)
	}
	if err := rebuild.ComparePromotionOutcome(facts.replay, live); err != nil {
		t.Fatalf("projection is not rebuildable from the ledger: %v", err)
	}
	if facts.provenance.Status != provenance.StatusComplete || facts.provenance.PublishedLedgerEventCount != 2 {
		t.Fatalf("provenance = %s (%d/%d)", facts.provenance.Status, facts.provenance.PublishedLedgerEventCount, facts.provenance.LedgerEventCount)
	}

	cases, _ := lc.GenerateCases(catalogInput())
	promo := caseByID(t, cases, lc.PromotionDefinition)
	res := lc.Evaluate(promo, facts.graph, authorized(s.tenant.String()))
	if res.Status != lc.StatusPartial {
		t.Fatalf("store-backed Promotion = %s %+v, want PARTIAL", res.Status, res.Findings)
	}
	for _, f := range res.Findings {
		if f.Code != lc.CodeLinkMissing || f.Link != lc.LinkReconciliation {
			t.Fatalf("store-backed Promotion has a finding beyond the missing reconciliation: %+v", f)
		}
	}
	for _, l := range res.Links {
		want := lc.StateProven
		if l.Link == lc.LinkReconciliation {
			want = lc.StateUnknown
		}
		if l.State != want {
			t.Fatalf("store link %s = %s, want %s", l.Link, l.State, want)
		}
	}

	// The redacted view a tenant reader without clearance gets carries the
	// same verdict and no restricted payload; a reader of another tenant
	// gets nothing.
	other := uuid.NewString()
	view := lc.View(facts.graph, uncleared(s.tenant.String()))
	if got := lc.Evaluate(promo, view, uncleared(s.tenant.String())); got.Status != lc.StatusPartial {
		t.Fatalf("redacted store view = %s %+v", got.Status, got.Findings)
	}
	if foreign := lc.View(facts.graph, authorized(other)); len(foreign.Records) != 0 {
		t.Fatal("store lineage disclosed to another tenant")
	}
	if got := lc.Evaluate(promo, facts.graph, authorized(other)); got.Status != lc.StatusDefective {
		t.Fatalf("store graph read by another tenant = %s", got.Status)
	}
}

// TestTodo_DATA_022_Recovery rebuilds the store-backed lineage on a fresh
// connection after the fact, requires the identical graph digest, refuses
// in-place rewrites of ledger and provenance history, and proves a second
// correction appends without disturbing any earlier record's seal.
func TestTodo_DATA_022_Recovery(t *testing.T) {
	s := newPromotionStore(t)
	s.record(t)
	trace := promotionTrace(t, s.tenant.String())
	first := s.readStoreGraph(t, s.db.Conn, trace)

	restarted := s.db.NewConn(t)
	second := s.readStoreGraph(t, restarted, trace)
	if lc.GraphDigest(first.graph) != lc.GraphDigest(second.graph) || first.replay.Digest != second.replay.Digest {
		t.Fatal("lineage rebuilt on a fresh connection differs from the original")
	}

	ctx := context.Background()
	if _, err := restarted.Exec(ctx, `UPDATE ledger_event SET payload = 'rewritten' WHERE tenant_id = $1 AND stream_key = $2 AND sequence = 1`, s.tenant, s.stream); err == nil {
		t.Fatal("ledger history was rewritten in place")
	}
	if _, err := restarted.Exec(ctx, `UPDATE provenance_record SET principal_ref = 'forged' WHERE tenant_id = $1`, s.tenant); err == nil {
		t.Fatal("provenance history was rewritten in place")
	}

	s.correct(t, 2, "second correction: effective date confirmed")
	third := s.readStoreGraph(t, restarted, trace)
	if f := lc.CheckAppendOnly(lc.PromotionDefinition, first.graph, third.graph); len(f) != 0 {
		t.Fatalf("appending a correction disturbed history: %+v", f)
	}
	corrections := 0
	for _, rec := range third.graph.Records {
		if rec.Link == lc.LinkCorrection {
			corrections++
		}
	}
	if corrections != 2 || len(third.graph.Records) != len(first.graph.Records)+1 {
		t.Fatalf("corrections = %d, records %d -> %d", corrections, len(first.graph.Records), len(third.graph.Records))
	}
	cases, _ := lc.GenerateCases(catalogInput())
	promo := caseByID(t, cases, lc.PromotionDefinition)
	res := lc.Evaluate(promo, third.graph, authorized(s.tenant.String()))
	if res.Status != lc.StatusPartial || linkState(res, lc.LinkCorrection) != lc.StateProven {
		t.Fatalf("lineage after second correction = %s %+v", res.Status, res.Findings)
	}

	// A lineage that "corrected" the original event in place is caught
	// against the recovered history.
	rewritten := withRecord(third.graph, lc.PromotionDefinition+"#EVENT", false, func(r *lc.Record) {
		r.SourceDigest = digestOf([]byte("rewritten"))
		r.Digest = r.ComputeDigest()
	})
	if f := lc.CheckAppendOnly(lc.PromotionDefinition, first.graph, rewritten); len(f) != 1 || f[0].Code != lc.CodeHistoryRewritten {
		t.Fatalf("in-place rewrite against recovered history = %+v", f)
	}
}
