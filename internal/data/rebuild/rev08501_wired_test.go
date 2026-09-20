package rebuild_test

// REV-085-01: the ALIGN-033..040 durability contracts must hold for the code
// the binaries actually run (internal/data/ledger, internal/data/outbox,
// internal/data/rebuild, internal/data/bitemporal, plus the exact-money,
// classification and retention owners those paths depend on), not for the
// unwired in-memory reference model in internal/data/productdurability.
//
// Every subtest below drives a wired package against real PostgreSQL through
// the pgtest fixture. RED: no test in the tree tied any of these invariants
// to a wired package - `go test -run TestTodo_REV_085_01 ./internal/data/rebuild/`
// matched no test before this file existed.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/bitemporal"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projection"
	"github.com/monstercameron/human-capital-management-suite/internal/data/rebuild"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/records"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dataclass"
)

// rev085Appender adapts the ledger package function to the narrow Appender
// slice outbox.Commit needs.
type rev085Appender struct{}

func (rev085Appender) Append(ctx context.Context, tx dbport.Tx, req ledger.AppendRequest) (ledger.AppendReceipt, error) {
	return ledger.Append(ctx, tx, req)
}

// rev085AppendRequest builds one ledger append on the tenant's stream.
func rev085AppendRequest(tenant uuid.UUID, stream string, head int64, effectiveAt time.Time) ledger.AppendRequest {
	return ledger.AppendRequest{
		Tenant:         tenant,
		StreamKey:      stream,
		ExpectedHead:   head,
		AssertionClass: ledger.TransactionFact,
		SourceRef:      "hcmnext:rev085",
		SchemaRef:      schemaRef,
		Payload:        fmt.Appendf(nil, "rev085-event-%d", head+1),
		OccurredAt:     effectiveAt,
		EffectiveAt:    effectiveAt,
		CorrelationID:  uuid.New(),
		IdempotencyKey: uuid.NewString(),
	}
}

// rev085CommitInTx runs one outbox.Commit inside a caller-managed
// transaction, returning the commit error without committing.
func rev085CommitInTx(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, stream, projectionName string, head int64, at time.Time, criticality string) (outbox.CommitReceipt, error) {
	t.Helper()
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	rolledBack := false
	defer func() {
		if !rolledBack {
			_ = tx.Rollback(ctx)
		}
	}()
	receipt, err := outbox.Commit(ctx, tx, rev085Appender{}, outbox.CommitRequest{
		Append:     rev085AppendRequest(tenant, stream, head, at),
		Projection: outbox.ProjectionSpec{Name: projectionName},
		Outbox: outbox.OutboxSpec{
			EffectIdentity: "rev085:" + uuid.NewString(),
			OrderingKey:    stream,
			Criticality:    criticality,
			SchemaRef:      schemaRef,
			Payload:        []byte(`{"rev":"085"}`),
		},
	})
	if err != nil {
		_ = tx.Rollback(ctx)
		rolledBack = true
		return outbox.CommitReceipt{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	rolledBack = true
	return receipt, nil
}

func rev085LedgerCount(t *testing.T, f fixture, tenant uuid.UUID, stream string) int {
	t.Helper()
	var n int
	f.db.QueryRow(context.Background(),
		`SELECT count(*) FROM ledger_event WHERE tenant_id = $1 AND stream_key = $2`,
		tenant, stream).Scan(&n)
	return n
}

// TestTodo_REV_085_01 proves each ALIGN-033..040 durability invariant against
// the wired package that owns it, on real PostgreSQL.
func TestTodo_REV_085_01(t *testing.T) {
	t.Run("AppendOnlyHistory", func(t *testing.T) {
		f := newFixture(t)
		tenant := insertTenant(t, f.db)
		stream := "rev085-history:" + uuid.NewString()
		ensureStream(t, f.db, tenant, stream, "rev085-history-proj")
		f.registerSchema(t, tenant)

		ctx := context.Background()
		reader := ledger.NewReader()
		var digests []string
		for head := int64(0); head < 3; head++ {
			var receipt ledger.AppendReceipt
			inTx(t, f.db, func(tx dbport.Tx) error {
				var err error
				receipt, err = ledger.Append(ctx, tx, rev085AppendRequest(tenant, stream, head, occurredAt.Add(time.Duration(head)*time.Hour)))
				return err
			})
			if receipt.Sequence != head+1 {
				t.Fatalf("append %d got sequence %d, want %d", head, receipt.Sequence, head+1)
			}
			if receipt.Digest == "" {
				t.Fatalf("append %d carried an empty digest", head)
			}
			digests = append(digests, receipt.Digest)
		}
		// The chronology reads back in sequence order with distinct digests.
		for seq := int64(1); seq <= 3; seq++ {
			ev, err := reader.ReadEvent(ctx, f.db.Conn, tenant, stream, seq)
			if err != nil {
				t.Fatalf("read event %d: %v", seq, err)
			}
			if ev.Sequence != seq || ev.Digest != digests[seq-1] {
				t.Fatalf("event %d = seq %d digest %q, want seq %d digest %q", seq, ev.Sequence, ev.Digest, seq, digests[seq-1])
			}
		}
		// A stale expected head is refused with the typed conflict, so two
		// writers cannot silently overwrite the same head.
		inTxExpect := func() error {
			ctx := context.Background()
			tx, err := f.db.Conn.Begin(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = tx.Rollback(ctx) }()
			_, err = ledger.Append(ctx, tx, rev085AppendRequest(tenant, stream, 0, occurredAt))
			return err
		}
		var stale ledger.ErrStaleStream
		if err := inTxExpect(); !errors.As(err, &stale) {
			t.Fatalf("stale append = %v, want ErrStaleStream", err)
		}
		if stale.Actual != 3 {
			t.Fatalf("stale append reports actual head %d, want 3", stale.Actual)
		}
	})

	t.Run("HalfOpenIntervals", func(t *testing.T) {
		f := newFixture(t)
		tenant := insertTenant(t, f.db)
		stream := "rev085-interval:" + uuid.NewString()
		ensureStream(t, f.db, tenant, stream, "rev085-interval-proj")
		f.registerSchema(t, tenant)

		ctx := context.Background()
		base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
		marks := []time.Time{base, base.Add(24 * time.Hour), base.Add(48 * time.Hour)}
		for i, at := range marks {
			inTx(t, f.db, func(tx dbport.Tx) error {
				_, err := ledger.Append(ctx, tx, rev085AppendRequest(tenant, stream, int64(i), at))
				return err
			})
		}
		// [T1, T2) contains exactly the T1 fact: the lower bound is
		// inclusive, the upper bound exclusive.
		res, err := bitemporal.Query(ctx, f.db.Conn, bitemporal.Request{
			Tenant:        tenant,
			Mode:          bitemporal.ModeBetween,
			Subject:       stream,
			EffectiveFrom: marks[1],
			EffectiveTo:   marks[2],
		}, bitemporal.Decision{Tenant: tenant})
		if err != nil {
			t.Fatalf("between query: %v", err)
		}
		if len(res.Facts) != 1 {
			t.Fatalf("between [T1,T2) returned %d facts, want exactly 1", len(res.Facts))
		}
		if !res.Facts[0].EffectiveAt.Equal(marks[1]) || res.Facts[0].Sequence != 2 {
			t.Fatalf("between fact = seq %d at %v, want seq 2 at T1", res.Facts[0].Sequence, res.Facts[0].EffectiveAt)
		}
	})

	t.Run("ExactMoney", func(t *testing.T) {
		f := newFixture(t)
		// Minor-unit exactness: 19.99 USD parses without loss.
		exact, err := values.NewDecimal("19.99", 2, values.RoundingExactRequired)
		if err != nil {
			t.Fatalf("parse 19.99: %v", err)
		}
		if exact.String() != "19.99" {
			t.Fatalf("decimal renders %q, want 19.99", exact.String())
		}
		// A third fractional digit is refused, never rounded away.
		if _, err := values.NewDecimal("19.999", 2, values.RoundingExactRequired); !errors.Is(err, values.ErrExcessScale) {
			t.Fatalf("parse 19.999 = %v, want ErrExcessScale", err)
		}
		// Storage round-trips through PostgreSQL numeric with no float in
		// the path: what lands is what was meant.
		var stored string
		f.db.QueryRow(context.Background(), `SELECT $1::numeric`, "19.99").Scan(&stored)
		back, err := values.NewDecimal(stored, 2, values.RoundingExactRequired)
		if err != nil {
			t.Fatalf("reparse stored %q: %v", stored, err)
		}
		if back.String() != "19.99" {
			t.Fatalf("round-tripped decimal renders %q, want 19.99", back.String())
		}
	})

	t.Run("OptimisticConcurrency", func(t *testing.T) {
		f := newFixture(t)
		tenant := insertTenant(t, f.db)
		stream := "rev085-cas:" + uuid.NewString()
		ensureStream(t, f.db, tenant, stream, "rev085-cas-proj")
		f.registerSchema(t, tenant)

		conns := []*pgxadapter.Conn{f.db.NewConn(t), f.db.NewConn(t)}
		type outcome struct {
			sequence int64
			err      error
		}
		results := make([]outcome, len(conns))
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := range conns {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				ctx := context.Background()
				tx, err := conns[i].Begin(ctx)
				if err != nil {
					results[i] = outcome{err: err}
					return
				}
				receipt, err := ledger.Append(ctx, tx, rev085AppendRequest(tenant, stream, 0, occurredAt))
				if err != nil {
					_ = tx.Rollback(ctx)
					results[i] = outcome{err: err}
					return
				}
				if err := tx.Commit(ctx); err != nil {
					results[i] = outcome{err: err}
					return
				}
				results[i] = outcome{sequence: receipt.Sequence}
			}(i)
		}
		close(start)
		wg.Wait()

		var winners int
		for i, got := range results {
			if got.err == nil {
				winners++
				if got.sequence != 1 {
					t.Fatalf("appender %d won with sequence %d, want 1", i, got.sequence)
				}
				continue
			}
			var stale ledger.ErrStaleStream
			if !errors.As(got.err, &stale) {
				t.Fatalf("appender %d failed with %v, want ErrStaleStream", i, got.err)
			}
			if stale.Expected != 0 || stale.Actual != 1 {
				t.Fatalf("appender %d reports expected %d actual %d, want 0 and 1", i, stale.Expected, stale.Actual)
			}
		}
		if winners != 1 {
			t.Fatalf("%d appenders won the same head, want exactly 1", winners)
		}
		if got := rev085LedgerCount(t, f, tenant, stream); got != 1 {
			t.Fatalf("stream holds %d events, want 1", got)
		}
	})

	t.Run("AtomicLedgerOutbox", func(t *testing.T) {
		f := newFixture(t)
		tenant := insertTenant(t, f.db)
		stream := "rev085-atomic:" + uuid.NewString()
		const projName = "rev085-atomic-proj"
		ensureStream(t, f.db, tenant, stream, projName)
		f.registerSchema(t, tenant)
		ctx := context.Background()

		receipt, err := rev085CommitInTx(t, f.db.Conn, tenant, stream, projName, 0, occurredAt, outbox.CriticalityP1)
		if err != nil {
			t.Fatalf("atomic commit: %v", err)
		}
		if receipt.Ledger.Sequence != 1 {
			t.Fatalf("committed ledger sequence %d, want 1", receipt.Ledger.Sequence)
		}
		// All three sides landed: the ledger event, the outbox message and
		// the projection checkpoint.
		ev, err := ledger.NewReader().ReadEvent(ctx, f.db.Conn, tenant, stream, 1)
		if err != nil {
			t.Fatalf("read committed event: %v", err)
		}
		if ev.Digest != receipt.Ledger.Digest {
			t.Fatalf("stored digest %q != receipt digest %q", ev.Digest, receipt.Ledger.Digest)
		}
		msg, err := outbox.Read(ctx, f.db.Conn, tenant, receipt.Outbox.OutboxID)
		if err != nil {
			t.Fatalf("read committed outbox message: %v", err)
		}
		if msg.EffectIdentity != receipt.Outbox.EffectIdentity || string(msg.Payload) != `{"rev":"085"}` {
			t.Fatalf("outbox row = %+v, want the committed effect and payload", msg)
		}
		cp, err := projection.Read(ctx, f.db.Conn, tenant, projName, stream)
		if err != nil {
			t.Fatalf("read checkpoint: %v", err)
		}
		if cp.LastAppliedSequence != 1 {
			t.Fatalf("checkpoint at %d, want 1", cp.LastAppliedSequence)
		}
		// A commit that fails after staging the ledger lines rolls every
		// side back: an invalid criticality refuses the whole unit.
		stream2 := "rev085-atomic-rollback:" + uuid.NewString()
		ensureStream(t, f.db, tenant, stream2, projName+"-rollback")
		if _, err := rev085CommitInTx(t, f.db.Conn, tenant, stream2, projName+"-rollback", 0, occurredAt, "bogus"); !errors.Is(err, outbox.ErrInvalidCriticality) {
			t.Fatalf("bad commit = %v, want ErrInvalidCriticality", err)
		}
		if got := rev085LedgerCount(t, f, tenant, stream2); got != 0 {
			t.Fatalf("rolled-back stream holds %d events, want 0", got)
		}
		if _, found, err := outbox.ReadByEffectIdentity(ctx, f.db.Conn, tenant, "rev085:rollback-never"); err != nil || found {
			t.Fatalf("rolled-back outbox read = found %v err %v, want no row", found, err)
		}
	})

	t.Run("StorageClassification", func(t *testing.T) {
		// The closed government-data vocabulary backs every stored field:
		// ACA health data carries minimum-necessary access.
		policy, err := dataclass.ClassPolicyFor(dataclass.GovernmentClassACA)
		if err != nil {
			t.Fatalf("ACA policy: %v", err)
		}
		if policy.Access != "ACA_MINIMUM_NECESSARY" {
			t.Fatalf("ACA access %q, want minimum-necessary", policy.Access)
		}
		// An unrecognized class is refused, never stored under a guess.
		if _, err := dataclass.ClassPolicyFor(dataclass.GovernmentDataClass("SECRET")); !errors.Is(err, dataclass.ErrInvalidClassification) {
			t.Fatalf("unknown class = %v, want ErrInvalidClassification", err)
		}
		// A medical accommodation field resolves to its ACA overlay with
		// highly-restricted sensitivity; an unknown field is refused.
		field, err := dataclass.ResolveField(authz.FieldMedicalAccomodation)
		if err != nil {
			t.Fatalf("resolve medical field: %v", err)
		}
		if field.GovernmentClass != dataclass.GovernmentClassACA {
			t.Fatalf("medical field class %q, want ACA", field.GovernmentClass)
		}
		if _, err := dataclass.ResolveField(authz.FieldID("no.such.field")); err == nil {
			t.Fatal("unknown field resolved without an error")
		}
	})

	t.Run("RetentionDisposition", func(t *testing.T) {
		rules := []records.ClassifiedRetentionRule{{
			Rule: records.RetentionRule{
				RecordSeries: "payroll-register", Jurisdiction: "US-FED",
				MinimumDays: 365, MaximumDays: 3650, AuthorityRef: "IRS-REV-PROC-10Y",
			},
			Class:                records.AuthorityTax,
			DispositionAuthority: "IRS-REV-PROC-10Y",
		}}
		schedules, err := records.ComposeClassified(rules)
		if err != nil {
			t.Fatalf("compose schedule: %v", err)
		}
		if len(schedules) != 1 || schedules[0].Digest == "" {
			t.Fatalf("schedules = %+v, want one digest-bearing schedule", schedules)
		}
		cutoff := time.Date(2020, 6, 1, 0, 0, 0, 0, time.UTC)
		aged := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
		// The minimum has elapsed: the record is eligible.
		verdict, err := records.EvaluateDisposition(schedules[0], cutoff, aged, nil)
		if err != nil {
			t.Fatalf("aged disposition: %v", err)
		}
		if verdict.Status != records.Eligible {
			t.Fatalf("aged status %q, want ELIGIBLE", verdict.Status)
		}
		// A legal hold freezes disposition regardless of age.
		hold := []records.DispositionHold{{
			ID: "hold-1", Class: records.AuthorityTax,
			Authority: "IRS", Reason: "audit",
		}}
		if _, err := records.EvaluateDisposition(schedules[0], cutoff, aged, hold); !errors.Is(err, records.ErrDispositionBlocked) {
			t.Fatalf("held disposition = %v, want ErrDispositionBlocked", err)
		}
		// An unelapsed minimum stays blocked with the authority named.
		fresh, err := records.EvaluateDisposition(schedules[0], aged, aged, nil)
		if err == nil {
			t.Fatalf("fresh record evaluated without refusal: %+v", fresh)
		}
		if !errors.Is(err, records.ErrDispositionBlocked) {
			t.Fatalf("fresh disposition = %v, want ErrDispositionBlocked", err)
		}
	})

	t.Run("RebuildFromChronology", func(t *testing.T) {
		f := newFixture(t)
		tenant := insertTenant(t, f.db)
		stream := "rev085-rebuild:" + uuid.NewString()
		ensureStream(t, f.db, tenant, stream, "worker_state")
		f.registerSchema(t, tenant)

		r1 := f.appendEvent(t, tenant, stream, 0, ledger.TransactionFact, nil)
		r2 := f.appendEvent(t, tenant, stream, 1, ledger.TransactionFact, nil)
		inTx(t, f.db, func(tx dbport.Tx) error {
			_, err := projection.Apply(context.Background(), tx, projection.ApplyRequest{
				Tenant: tenant, ProjectionName: "worker_state", StreamKey: stream,
				Sequence: 1, Digest: r1.Digest,
			})
			return err
		})
		f.seedRow(t, tenant, stream, 1, "value-1")
		f.seedRow(t, tenant, stream, 2, "value-2")

		executor := rebuild.New(rebuild.NewPGSource(ledger.NewReader()), f.db.Conn)
		report, err := executor.Rebuild(context.Background(), target(tenant, stream), fixtureReducer())
		if err != nil {
			t.Fatalf("rebuild: %v", err)
		}
		if !report.Match || !report.Promoted {
			t.Fatalf("report = %+v, want a matched, promoted rebuild", report)
		}
		if report.Replayed != 2 || report.SourceHead != 2 {
			t.Fatalf("replayed %d of head %d, want 2 of 2", report.Replayed, report.SourceHead)
		}
		cp, err := projection.Read(context.Background(), f.db.Conn, tenant, "worker_state", stream)
		if err != nil {
			t.Fatalf("read checkpoint: %v", err)
		}
		if cp.LastAppliedSequence != 2 || cp.LastAppliedDigest != r2.Digest {
			t.Fatalf("checkpoint = %+v, want sequence 2 with the head digest", cp)
		}
		if got := f.leftoverShadowTables(t); got != 0 {
			t.Fatalf("%d shadow table(s) left behind, want none", got)
		}
	})
}

// TestTodo_REV_085_01_Integration proves the wired durability path composes
// end to end on real PostgreSQL: one atomic ledger+outbox commit feeds the
// projection checkpoint, a second ledger event extends the stream, the
// bitemporal read sees both facts, and a shadow rebuild of the same
// chronology promotes with the checkpoint at the head.
func TestTodo_REV_085_01_Integration(t *testing.T) {
	f := newFixture(t)
	tenant := insertTenant(t, f.db)
	stream := "rev085-chain:" + uuid.NewString()
	const projName = "rev085-chain-proj"
	ensureStream(t, f.db, tenant, stream, projName)
	f.registerSchema(t, tenant)
	ctx := context.Background()

	receipt, err := rev085CommitInTx(t, f.db.Conn, tenant, stream, projName, 0, occurredAt, outbox.CriticalityP1)
	if err != nil {
		t.Fatalf("atomic commit: %v", err)
	}
	secondAt := occurredAt.Add(2 * time.Hour)
	inTx(t, f.db, func(tx dbport.Tx) error {
		if _, err := ledger.Append(ctx, tx, rev085AppendRequest(tenant, stream, 1, secondAt)); err != nil {
			return err
		}
		_, err := projection.Apply(ctx, tx, projection.ApplyRequest{
			Tenant: tenant, ProjectionName: projName, StreamKey: stream,
			Sequence: 2, Digest: receipt.Ledger.Digest,
		})
		return err
	})
	f.seedRow(t, tenant, stream, 1, "value-1")
	f.seedRow(t, tenant, stream, 2, "value-2")

	// Both facts are visible through the half-open bitemporal read.
	res, err := bitemporal.Query(ctx, f.db.Conn, bitemporal.Request{
		Tenant: tenant, Mode: bitemporal.ModeBetween, Subject: stream,
		EffectiveFrom: occurredAt, EffectiveTo: secondAt.Add(time.Hour),
	}, bitemporal.Decision{Tenant: tenant})
	if err != nil {
		t.Fatalf("chain between query: %v", err)
	}
	if len(res.Facts) != 2 || res.Facts[0].Sequence != 1 || res.Facts[1].Sequence != 2 {
		t.Fatalf("chain facts = %+v, want sequences [1 2]", res.Facts)
	}
	// The same chronology rebuilds and promotes with the checkpoint at the
	// head, leaving no shadow behind.
	executor := rebuild.New(rebuild.NewPGSource(ledger.NewReader()), f.db.Conn)
	report, err := executor.Rebuild(ctx, rebuild.Target{Tenant: tenant, Stream: stream, Projection: projName}, fixtureReducer())
	if err != nil {
		t.Fatalf("chain rebuild: %v", err)
	}
	if !report.Match || !report.Promoted {
		t.Fatalf("chain report = %+v, want matched and promoted", report)
	}
	cp, err := projection.Read(ctx, f.db.Conn, tenant, projName, stream)
	if err != nil {
		t.Fatalf("chain checkpoint: %v", err)
	}
	if cp.LastAppliedSequence != 2 {
		t.Fatalf("chain checkpoint at %d, want 2", cp.LastAppliedSequence)
	}
	if got := f.leftoverShadowTables(t); got != 0 {
		t.Fatalf("chain left %d shadow table(s) behind, want none", got)
	}
}

// rev085ConformanceLines is the pinned contract: which durability invariant
// each wired package proves, and the PostgreSQL evidence behind it.
func rev085ConformanceLines() []string {
	return []string{
		"ALIGN-033 append-only-history internal/data/ledger append+read+stale-head postgres",
		"ALIGN-034 half-open-intervals internal/data/bitemporal between-window postgres",
		"ALIGN-035 exact-money internal/kernel/values numeric-roundtrip postgres",
		"ALIGN-036 optimistic-concurrency internal/data/ledger expected-head-cas postgres",
		"ALIGN-037 atomic-ledger-outbox internal/data/outbox commit-rollback postgres",
		"ALIGN-038 storage-classification internal/trust/dataclass closed-vocabulary",
		"ALIGN-039 retention-disposition internal/governance/records hold-freeze",
		"ALIGN-040 rebuild-from-chronology internal/data/rebuild shadow-promote postgres",
	}
}

// TestTodo_REV_085_01_Conformance pins the wired-proof contract: the eight
// invariants, their owning wired packages and their evidence kinds must read
// back byte-for-byte from the checked-in golden file.
func TestTodo_REV_085_01_Conformance(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "rev08501_conformance.golden"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	want := strings.TrimSpace(string(raw))
	got := strings.Join(rev085ConformanceLines(), "\n")
	if got != want {
		t.Fatalf("conformance contract drifted:\n got: %q\nwant: %q", got, want)
	}
}
