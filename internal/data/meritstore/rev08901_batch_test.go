package meritstore_test

// REV-089-01 (meritstore half): EnqueueCompensationChangeIntentsTx batches
// its per-child INSERT into one dbport.ExecAll call. The integration test
// proves a multi-child finalize persists every child and re-enqueues to zero
// on real PostgreSQL; the benchmark pins the one-batch-per-finalize shape.

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/meritstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/merit"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// rev089TB is the tiny tester surface both *testing.T and *testing.B offer,
// so one cycle builder serves the integration test and the benchmark.
type rev089TB interface {
	Helper()
	Fatalf(string, ...any)
}

func rev089Cycle(tb rev089TB, id string, workers int) merit.MeritCycle {
	tb.Helper()
	money, err := values.NewMoney("100.00", "USD", 2, values.RoundingHalfEven)
	if err != nil {
		tb.Fatalf("money: %v", err)
	}
	watermark, err := values.NewSequenceRevision("merit-population", 1)
	if err != nil {
		tb.Fatalf("watermark: %v", err)
	}
	members := make([]merit.PopulationMember, 0, workers)
	for i := 0; i < workers; i++ {
		worker := fmt.Sprintf("worker-%d", i+1)
		members = append(members, merit.PopulationMember{
			ParticipantID: worker, ManagerID: "manager-1", BasePay: money,
			PerformanceRating: values.MustDecimal("4.00", 2, values.RoundingHalfEven),
			BandPosition:      values.MustDecimal("0.50", 2, values.RoundingHalfEven),
			SalaryRevisionRef: "salary-1", PerformanceRef: "performance-1",
			EffectiveAt: testInstant(), KnownAt: testInstant(),
		})
	}
	population, err := merit.NewPopulationSnapshot(merit.PopulationSnapshot{
		SnapshotID: "population-" + id, Revision: 1, Frozen: true,
		FrozenAt: testInstant(), Watermark: watermark, Members: members,
	})
	if err != nil {
		tb.Fatalf("population: %v", err)
	}
	guidelines, err := merit.NewGuidelineMatrix(merit.GuidelineMatrix{
		MatrixID: "matrix-1", Version: "1", Rules: []merit.GuidelineRule{{
			RatingMin:       values.MustDecimal("4.00", 2, values.RoundingHalfEven),
			RatingMax:       values.MustDecimal("4.00", 2, values.RoundingHalfEven),
			BandPositionMin: values.MustDecimal("0.00", 2, values.RoundingHalfEven),
			BandPositionMax: values.MustDecimal("1.00", 2, values.RoundingHalfEven),
			MinimumRate:     values.MustDecimal("0.05", 2, values.RoundingHalfEven),
			MaximumRate:     values.MustDecimal("0.10", 2, values.RoundingHalfEven),
		}},
	})
	if err != nil {
		tb.Fatalf("guidelines: %v", err)
	}
	cycle, err := merit.NewMeritCycle(merit.MeritCycle{
		CycleID: id, Revision: 1, Population: population, Guidelines: guidelines,
		Budget:   values.MustDecimal(fmt.Sprintf("%d.00", workers*10), 2, values.RoundingHalfEven),
		Currency: "USD", State: merit.CycleDraft,
		EffectiveAt: testInstant(), KnownAt: testInstant(),
	})
	if err != nil {
		tb.Fatalf("cycle: %v", err)
	}
	return cycle
}

func rev089Finalize(tb rev089TB, cycle merit.MeritCycle, workers int) merit.MeritCycle {
	tb.Helper()
	verifier := decisionVerifier{receipts: map[string]string{}}
	proposed := make([]merit.MeritCycle, 0, workers)
	recs := make([]merit.MeritRecommendation, 0, workers)
	cur := cycle
	for i := 0; i < workers; i++ {
		worker := fmt.Sprintf("worker-%d", i+1)
		next, rec, err := cur.Propose(worker, values.MustDecimal("0.05", 2, values.RoundingHalfEven), "manager-1")
		if err != nil {
			tb.Fatalf("propose %s: %v", worker, err)
		}
		proposed = append(proposed, next)
		recs = append(recs, rec)
		cur = next
	}
	cur = proposed[workers-1]
	for i := 0; i < workers; i++ {
		worker := fmt.Sprintf("worker-%d", i+1)
		receipt := merit.ApprovalReceipt{ReceiptID: "signed-" + worker, ApproverID: "approver-1", RecommendationDigest: recs[i].CanonicalDigest}
		verifier.receipts[receipt.ReceiptID] = receipt.RecommendationDigest
		approved, err := cur.Approve(worker, receipt, verifier)
		if err != nil {
			tb.Fatalf("approve %s: %v", worker, err)
		}
		cur = approved
	}
	finalized, err := cur.Finalize()
	if err != nil {
		tb.Fatalf("finalize: %v", err)
	}
	return finalized
}

// rev089Row serves revision-check scans from the finalized cycle.
type rev089Row struct {
	digest string
	state  string
}

func (r rev089Row) Scan(dest ...any) error {
	if len(dest) == 1 {
		return dbport.ErrNoRows
	}
	if len(dest) != 2 {
		return errors.New("rev089: unexpected scan arity")
	}
	digest, ok := dest[0].(*string)
	if !ok {
		return errors.New("rev089: digest destination is not a string pointer")
	}
	state, ok := dest[1].(*string)
	if !ok {
		return errors.New("rev089: state destination is not a string pointer")
	}
	*digest = r.digest
	*state = r.state
	return nil
}

type rev089Results struct{}

func (rev089Results) Exec() (int64, error) { return 1, nil }
func (rev089Results) QueryRow() dbport.Row { return rev089Row{} }
func (rev089Results) Close() error         { return nil }

type rev089Batch struct{ tx *rev089Tx }

func (b *rev089Batch) Queue(string, ...any) { b.tx.queued++ }

// rev089Tx is a Batcher tx double: the revision check reads back the
// finalized cycle, every batched insert reports one fresh row.
type rev089Tx struct {
	digest  string
	state   string
	batches int
	queued  int
	execs   int
}

func (tx *rev089Tx) Exec(context.Context, string, ...any) (int64, error) {
	tx.execs++
	return 1, nil
}

func (tx *rev089Tx) Query(context.Context, string, ...any) (dbport.Rows, error) {
	return nil, errors.New("rev089: unexpected query")
}

func (tx *rev089Tx) QueryRow(context.Context, string, ...any) dbport.Row {
	return rev089Row{digest: tx.digest, state: tx.state}
}

func (tx *rev089Tx) SendBatch(_ context.Context, fn func(dbport.Batch)) (dbport.BatchResults, error) {
	tx.batches++
	fn(&rev089Batch{tx: tx})
	return rev089Results{}, nil
}

func (tx *rev089Tx) Commit(context.Context) error   { return nil }
func (tx *rev089Tx) Rollback(context.Context) error { return nil }

// TestTodo_REV_089_01_Integration proves the batched emission path on real
// PostgreSQL: a three-child finalize persists three rows, and re-enqueuing
// the same finalize stores nothing new.
func TestTodo_REV_089_01_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db)
	store := meritstore.New(appConn(t, db))
	ctx := context.Background()

	base := rev089Cycle(t, "rev089-multi", 3)
	if err := store.Save(ctx, tenant.String(), base); err != nil {
		t.Fatalf("save base: %v", err)
	}
	finalized := base
	verifier := decisionVerifier{receipts: map[string]string{}}
	proposed := make([]merit.MeritCycle, 0, 3)
	recs := make([]merit.MeritRecommendation, 0, 3)
	for i := 0; i < 3; i++ {
		worker := fmt.Sprintf("worker-%d", i+1)
		next, rec, err := finalized.Propose(worker, values.MustDecimal("0.05", 2, values.RoundingHalfEven), "manager-1")
		if err != nil {
			t.Fatalf("propose %s: %v", worker, err)
		}
		if err := store.Save(ctx, tenant.String(), next); err != nil {
			t.Fatalf("save proposed %s: %v", worker, err)
		}
		proposed = append(proposed, next)
		recs = append(recs, rec)
		finalized = next
	}
	finalized = proposed[2]
	for i := 0; i < 3; i++ {
		worker := fmt.Sprintf("worker-%d", i+1)
		receipt := merit.ApprovalReceipt{ReceiptID: "signed-" + worker, ApproverID: "approver-1", RecommendationDigest: recs[i].CanonicalDigest}
		verifier.receipts[receipt.ReceiptID] = receipt.RecommendationDigest
		approved, err := finalized.Approve(worker, receipt, verifier)
		if err != nil {
			t.Fatalf("approve %s: %v", worker, err)
		}
		if err := store.Save(ctx, tenant.String(), approved); err != nil {
			t.Fatalf("save approved %s: %v", worker, err)
		}
		finalized = approved
	}
	done, err := finalized.Finalize()
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if err := store.Save(ctx, tenant.String(), done); err != nil {
		t.Fatalf("save finalized: %v", err)
	}
	finalized = done
	conn := appConn(t, db)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		t.Fatalf("tenant scope: %v", err)
	}
	fresh, err := store.EnqueueCompensationChangeIntentsTx(ctx, tx, tenant.String(), finalized)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if len(fresh) != 3 {
		t.Fatalf("fresh children = %d, want 3", len(fresh))
	}
	var stored int
	db.QueryRow(ctx, `SELECT count(*) FROM merit_compensation_intent_emission WHERE tenant_id = $1`, tenant).Scan(&stored)
	if stored != 3 {
		t.Fatalf("stored emission rows = %d, want 3", stored)
	}

	tx2, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := tenancy.WithTenant(ctx, tx2, tenant); err != nil {
		t.Fatalf("tenant scope: %v", err)
	}
	again, err := store.EnqueueCompensationChangeIntentsTx(ctx, tx2, tenant.String(), finalized)
	if err != nil {
		t.Fatalf("re-enqueue: %v", err)
	}
	if err := tx2.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("re-enqueue fresh children = %d, want 0", len(again))
	}
	db.QueryRow(ctx, `SELECT count(*) FROM merit_compensation_intent_emission WHERE tenant_id = $1`, tenant).Scan(&stored)
	if stored != 3 {
		t.Fatalf("stored emission rows after re-enqueue = %d, want 3", stored)
	}
}

// BenchmarkTodo_REV_089_01 pins the one-batch-per-finalize shape: a
// five-child finalize issues exactly one SendBatch carrying five statements,
// where the row-at-a-time loop issued five Exec round trips.
func BenchmarkTodo_REV_089_01(b *testing.B) {
	finalized := rev089Finalize(b, rev089Cycle(b, "rev089-bench", 5), 5)
	children, err := finalized.CompensationChangeIntents()
	if err != nil {
		b.Fatalf("children: %v", err)
	}
	if len(children) != 5 {
		b.Fatalf("children = %d, want 5", len(children))
	}
	var store meritstore.Store
	once := &rev089Tx{digest: finalized.CanonicalDigest, state: string(merit.CycleFinalized)}
	if _, err := store.EnqueueCompensationChangeIntentsTx(context.Background(), once, uuid.NewString(), finalized); err != nil {
		b.Fatalf("enqueue: %v", err)
	}
	if once.batches != 1 || once.queued != 5 || once.execs != 0 {
		b.Fatalf("batches=%d queued=%d execs=%d, want one batch of five with no row-at-a-time Exec", once.batches, once.queued, once.execs)
	}
	b.ReportMetric(float64(once.queued)/float64(once.batches), "statements/batch")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tx := &rev089Tx{digest: finalized.CanonicalDigest, state: string(merit.CycleFinalized)}
		if _, err := store.EnqueueCompensationChangeIntentsTx(context.Background(), tx, uuid.NewString(), finalized); err != nil {
			b.Fatalf("enqueue: %v", err)
		}
	}
}
