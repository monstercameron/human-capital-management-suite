package meritstore_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/meritstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/merit"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/performance"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type decisionVerifier struct{ receipts map[string]string }

type preflightSignalDB struct {
	db     dbport.Beginner
	signal chan struct{}
	once   *sync.Once
}

func (d preflightSignalDB) Begin(ctx context.Context) (dbport.Tx, error) {
	tx, err := d.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &preflightSignalTx{Tx: tx, signal: d.signal, once: d.once}, nil
}

type preflightSignalTx struct {
	dbport.Tx
	signal chan struct{}
	once   *sync.Once
}

func (t *preflightSignalTx) Query(ctx context.Context, query string, args ...any) (dbport.Rows, error) {
	rows, err := t.Tx.Query(ctx, query, args...)
	if strings.Contains(query, "SELECT intent_id,canonical_digest FROM merit_compensation_intent_emission") {
		t.once.Do(func() { close(t.signal) })
	}
	return rows, err
}

func (t *preflightSignalTx) QueryRow(ctx context.Context, query string, args ...any) dbport.Row {
	row := t.Tx.QueryRow(ctx, query, args...)
	if strings.Contains(query, "SELECT canonical_digest FROM merit_compensation_intent_emission") {
		return preflightSignalRow{Row: row, signal: t.signal, once: t.once}
	}
	return row
}

type preflightSignalRow struct {
	dbport.Row
	signal chan struct{}
	once   *sync.Once
}

func (r preflightSignalRow) Scan(dest ...any) error {
	err := r.Row.Scan(dest...)
	if errors.Is(err, dbport.ErrNoRows) {
		r.once.Do(func() { close(r.signal) })
	}
	return err
}

func (v decisionVerifier) verify(r merit.ApprovalReceipt) error {
	if v.receipts[r.ReceiptID] != r.RecommendationDigest {
		return errors.New("receipt signature mismatch")
	}
	return nil
}
func (v decisionVerifier) VerifyMeritApproval(r merit.ApprovalReceipt) error   { return v.verify(r) }
func (v decisionVerifier) VerifyMeritRejection(r merit.RejectionReceipt) error { return v.verify(r) }
func (v decisionVerifier) VerifyStoredMeritDecision(r merit.MeritRecommendation, e merit.StoredDecisionEvidence) error {
	receipt, digest := e.ApprovalReceiptID, e.ApprovedDigest
	if r.State == merit.RecommendationRejected {
		receipt, digest = e.RejectionReceiptID, e.RejectedDigest
	}
	if v.receipts[receipt] != digest {
		return errors.New("stored receipt signature mismatch")
	}
	return nil
}

func TestMain(m *testing.M) { pgtest.RunMain(m) }

const meritDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func insertTenant(t *testing.T, db *pgtest.DB) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, id, "merit-"+id.String(), "merit-"+id.String())
	return id
}

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("SET ROLE: %v", err)
	}
	return conn
}

func tenantTxErr(conn *pgxadapter.Conn, tenant uuid.UUID, fn func(dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

func testInstant() values.Instant {
	return values.NewInstant(time.Date(2026, 9, 6, 12, 0, 0, 987654321, time.UTC))
}

func testDecimal(t *testing.T, text string) values.Decimal {
	t.Helper()
	return values.MustDecimal(text, 2, values.RoundingHalfEven)
}

func testCycle(t *testing.T, id string) merit.MeritCycle {
	t.Helper()
	money, err := values.NewMoney("100.00", "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	watermark, err := values.NewSequenceRevision("merit-population", 1)
	if err != nil {
		t.Fatal(err)
	}
	population, err := merit.NewPopulationSnapshot(merit.PopulationSnapshot{
		SnapshotID: "population-" + id, Revision: 1, Frozen: true, FrozenAt: testInstant(), Watermark: watermark,
		Members: []merit.PopulationMember{{ParticipantID: "worker-1", ManagerID: "manager-1", BasePay: money,
			PerformanceRating: testDecimal(t, "4.00"), BandPosition: testDecimal(t, "0.50"), SalaryRevisionRef: "salary-1",
			PerformanceRef: "performance-1", EffectiveAt: testInstant(), KnownAt: testInstant()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	guidelines, err := merit.NewGuidelineMatrix(merit.GuidelineMatrix{
		MatrixID: "matrix-1", Version: "1", Rules: []merit.GuidelineRule{{
			RatingMin: testDecimal(t, "4.00"), RatingMax: testDecimal(t, "4.00"), BandPositionMin: testDecimal(t, "0.00"), BandPositionMax: testDecimal(t, "1.00"), MinimumRate: testDecimal(t, "0.05"), MaximumRate: testDecimal(t, "0.10"),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	cycle, err := merit.NewMeritCycle(merit.MeritCycle{
		CycleID: id, Revision: 1, Population: population, Guidelines: guidelines, Budget: testDecimal(t, "20.00"),
		Currency: "USD", State: merit.CycleDraft, EffectiveAt: testInstant(), KnownAt: testInstant(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return cycle
}

func TestTodo_PERSIST_MERIT_001(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db)
	store := meritstore.New(appConn(t, db))
	base := testCycle(t, "cycle-primary")
	if err := store.Save(context.Background(), tenant.String(), base); err != nil {
		t.Fatal(err)
	}
	cycle, recommendation, err := base.Propose("worker-1", testDecimal(t, "0.05"), "manager-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), tenant.String(), cycle); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load(context.Background(), tenant.String(), cycle.CycleID, cycle.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if got.CanonicalDigest != cycle.CanonicalDigest || got.Population.CanonicalDigest != base.Population.CanonicalDigest || len(got.Recommendations) != 1 || got.Recommendations[0].CanonicalDigest != recommendation.CanonicalDigest {
		t.Fatalf("round trip = %+v, want cycle digest %s and recommendation %s", got, cycle.CanonicalDigest, recommendation.CanonicalDigest)
	}
}

func TestTodo_PERSIST_MERIT_001_Fault(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db)
	store := meritstore.New(appConn(t, db))
	base := testCycle(t, "cycle-fault")
	if err := store.Save(context.Background(), tenant.String(), base); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), tenant.String(), base); !errors.Is(err, merit.ErrStoreDuplicate) {
		t.Fatalf("duplicate = %v, want ErrStoreDuplicate", err)
	}
	next, _, err := base.Propose("worker-1", testDecimal(t, "0.05"), "manager-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), tenant.String(), next); err != nil {
		t.Fatal(err)
	}
	stale := next
	stale.Revision = 3
	stale.ParentRevision = 1
	stale.ParentDigest = base.CanonicalDigest
	stale.CanonicalDigest = ""
	err = store.Save(context.Background(), tenant.String(), stale)
	if !errors.Is(err, merit.ErrStoreStaleCAS) {
		t.Fatalf("stale = %v, want ErrStoreStaleCAS", err)
	}
	var typed *merit.StoreError
	if !errors.As(err, &typed) || typed.Code != merit.StoreStaleCASCode {
		t.Fatalf("stale error = %T/%v, want typed stale-CAS code", err, err)
	}
}

func TestTodo_PERSIST_MERIT_001_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db)
	base := testCycle(t, "cycle-integration")
	writer := meritstore.New(appConn(t, db))
	if err := writer.Save(context.Background(), tenant.String(), base); err != nil {
		t.Fatal(err)
	}
	cycle, _, err := base.Propose("worker-1", testDecimal(t, "0.10"), "manager-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Save(context.Background(), tenant.String(), cycle); err != nil {
		t.Fatal(err)
	}
	reader := meritstore.New(appConn(t, db))
	got, err := reader.Current(context.Background(), tenant.String(), base.CycleID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != 2 || got.Recommendations[0].Amount.String() != "10.00" {
		t.Fatalf("fresh connection current = %+v, want revision 2 and amount 10.00", got)
	}
	calibrated, err := cycle.Calibrate("worker-1", merit.CalibrationAdjustment{
		ParticipantID: "worker-1", From: testDecimal(t, "10.00"), To: testDecimal(t, "11.00"),
		Reason: performance.CalibrationReasonEvidence, AdjusterID: "peer-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Save(context.Background(), tenant.String(), calibrated); err != nil {
		t.Fatal(err)
	}
	got, err = meritstore.New(appConn(t, db)).Current(context.Background(), tenant.String(), base.CycleID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != calibrated.Revision || got.Recommendations[0].Amount.String() != "11.00" || len(got.Recommendations[0].Adjustments) != 1 {
		t.Fatalf("calibrated current = %+v, want revision %d and one 11.00 adjustment", got, calibrated.Revision)
	}
}

func TestTodo_PERSIST_MERIT_001_Security(t *testing.T) {
	db := pgtest.New(t)
	alpha, beta := insertTenant(t, db), insertTenant(t, db)
	alphaStore := meritstore.New(appConn(t, db))
	cycle := testCycle(t, "cycle-security")
	if err := alphaStore.Save(context.Background(), alpha.String(), cycle); err != nil {
		t.Fatal(err)
	}
	betaConn := appConn(t, db)
	betaStore := meritstore.New(betaConn)
	if _, err := betaStore.Current(context.Background(), beta.String(), cycle.CycleID); !errors.Is(err, merit.ErrStoreNotFound) {
		t.Fatalf("cross-tenant current = %v, want ErrStoreNotFound", err)
	}
	var count int
	if err := tenantTxErr(betaConn, beta, func(tx dbport.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM merit_cycle_revision`).Scan(&count)
	}); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("beta saw %d alpha cycle rows", count)
	}
}

func TestTodo_PERSIST_MERIT_001_Recovery(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db)
	cycle := testCycle(t, "cycle-recovery")
	if err := meritstore.New(appConn(t, db)).Save(context.Background(), tenant.String(), cycle); err != nil {
		t.Fatal(err)
	}
	got, err := meritstore.New(appConn(t, db)).Load(context.Background(), tenant.String(), cycle.CycleID, cycle.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if got.CanonicalDigest != cycle.CanonicalDigest || got.Population.SnapshotID != cycle.Population.SnapshotID {
		t.Fatalf("recovered cycle = %+v, want digest %s and snapshot %s", got, cycle.CanonicalDigest, cycle.Population.SnapshotID)
	}
}

func TestMeritDecisionEvidenceRoundTrip(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db)
	verifier := decisionVerifier{receipts: make(map[string]string)}
	store := meritstore.NewWithDecisionVerifier(appConn(t, db), verifier)
	ctx := context.Background()
	base := testCycle(t, "cycle-decision-roundtrip")
	if err := store.Save(ctx, tenant.String(), base); err != nil {
		t.Fatal(err)
	}
	proposed, rec, err := base.Propose("worker-1", testDecimal(t, "0.05"), "manager-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, tenant.String(), proposed); err != nil {
		t.Fatal(err)
	}
	receipt := merit.ApprovalReceipt{ReceiptID: "signed-approval", ApproverID: "approver-1", RecommendationDigest: rec.CanonicalDigest}
	verifier.receipts[receipt.ReceiptID] = receipt.RecommendationDigest
	approved, err := proposed.Approve("worker-1", receipt, verifier)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, tenant.String(), approved); err != nil {
		t.Fatal(err)
	}
	finalized, err := approved.Finalize()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, tenant.String(), finalized); err != nil {
		t.Fatal(err)
	}
	rollbackConn := appConn(t, db)
	rollbackTx, err := rollbackConn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(ctx, rollbackTx, tenant); err != nil {
		t.Fatal(err)
	}
	rolledBackFresh, err := store.EnqueueCompensationChangeIntentsTx(ctx, rollbackTx, tenant.String(), finalized)
	if err != nil || len(rolledBackFresh) != 1 {
		t.Fatalf("caller transaction enqueue=%#v err=%v", rolledBackFresh, err)
	}
	if err := rollbackTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	type emissionResult struct {
		fresh []merit.CompensationChangeIntent
		err   error
	}
	results := make(chan emissionResult, 2)
	emitters := []*meritstore.Store{meritstore.New(appConn(t, db)), meritstore.New(appConn(t, db))}
	for _, emitter := range emitters {
		go func(emitter *meritstore.Store) {
			fresh, err := emitter.EnqueueCompensationChangeIntents(ctx, tenant.String(), finalized)
			results <- emissionResult{fresh, err}
		}(emitter)
	}
	freshCount := 0
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		freshCount += len(result.fresh)
	}
	if freshCount != 1 {
		t.Fatalf("concurrent durable emissions=%d", freshCount)
	}
	restarted := meritstore.New(appConn(t, db))
	if fresh, err := restarted.EnqueueCompensationChangeIntents(ctx, tenant.String(), finalized); err != nil || len(fresh) != 0 {
		t.Fatalf("restart retry=%#v err=%v", fresh, err)
	}
	stored, err := restarted.StoredCompensationChangeIntents(ctx, tenant.String(), finalized.CycleID, finalized.Revision)
	wantStored, errWant := finalized.CompensationChangeIntents()
	if err != nil || errWant != nil || len(stored) != 1 || stored[0].IntentID != wantStored[0].IntentID || stored[0].CanonicalDigest != wantStored[0].CanonicalDigest {
		t.Fatalf("stored children=%#v err=%v", stored, err)
	}
	otherTenant := insertTenant(t, db)
	if _, err := restarted.EnqueueCompensationChangeIntents(ctx, otherTenant.String(), finalized); !errors.Is(err, merit.ErrStoreNotFound) {
		t.Fatalf("cross-tenant fabricated source=%v", err)
	}
	conflictBase := testCycle(t, "cycle-emission-conflict")
	if err := store.Save(ctx, tenant.String(), conflictBase); err != nil {
		t.Fatal(err)
	}
	conflictProposed, conflictRec, err := conflictBase.Propose("worker-1", testDecimal(t, "0.05"), "manager-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, tenant.String(), conflictProposed); err != nil {
		t.Fatal(err)
	}
	conflictReceipt := merit.ApprovalReceipt{ReceiptID: "signed-conflict", ApproverID: "approver-1", RecommendationDigest: conflictRec.CanonicalDigest}
	verifier.receipts[conflictReceipt.ReceiptID] = conflictReceipt.RecommendationDigest
	conflictApproved, err := conflictProposed.Approve("worker-1", conflictReceipt, verifier)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, tenant.String(), conflictApproved); err != nil {
		t.Fatal(err)
	}
	conflictFinal, err := conflictApproved.Finalize()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, tenant.String(), conflictFinal); err != nil {
		t.Fatal(err)
	}
	conflictChildren, err := conflictFinal.CompensationChangeIntents()
	if err != nil {
		t.Fatal(err)
	}
	blockingConn := appConn(t, db)
	blockingTx, err := blockingConn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(ctx, blockingTx, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := blockingTx.Exec(ctx, `INSERT INTO merit_compensation_intent_emission
		(tenant_id,intent_id,cycle_id,cycle_revision,participant_id,payload,canonical_digest,source_digest)
		VALUES ($1,$2,$3,$4,$5,'{}',$6,$7)`, tenant, conflictChildren[0].IntentID, conflictFinal.CycleID, conflictFinal.Revision,
		conflictChildren[0].ParticipantID, strings.Repeat("d", 64), conflictFinal.CanonicalDigest[len("sha256:"):]); err != nil {
		t.Fatal(err)
	}
	preflight := make(chan struct{})
	conflictingEmitter := meritstore.New(preflightSignalDB{db: appConn(t, db), signal: preflight, once: &sync.Once{}})
	conflictResult := make(chan error, 1)
	go func() {
		_, err := conflictingEmitter.EnqueueCompensationChangeIntents(ctx, tenant.String(), conflictFinal)
		conflictResult <- err
	}()
	select {
	case <-preflight:
	case <-time.After(5 * time.Second):
		t.Fatal("losing emitter did not complete its preflight lookup")
	}
	if err := blockingTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-conflictResult; !errors.Is(err, merit.ErrIntentConflict) {
		t.Fatalf("contended payload conflict=%v", err)
	}
	got, err := meritstore.NewWithDecisionVerifier(appConn(t, db), verifier).Current(ctx, tenant.String(), base.CycleID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CanonicalDigest != finalized.CanonicalDigest || got.Recommendations[0].ApprovedDigest != rec.CanonicalDigest || got.Recommendations[0].ApprovalReceiptID != receipt.ReceiptID || got.Recommendations[0].EffectRevision != finalized.Revision {
		t.Fatalf("finalized decision round trip=%#v", got.Recommendations[0])
	}
	if _, err := meritstore.New(appConn(t, db)).Current(ctx, tenant.String(), base.CycleID); !errors.Is(err, merit.ErrStoreInvalid) {
		t.Fatalf("unverified rehydration=%v", err)
	}
}

func TestMeritRejectedDecisionRoundTripAndTampering(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db)
	otherTenant := insertTenant(t, db)
	verifier := decisionVerifier{receipts: make(map[string]string)}
	store := meritstore.NewWithDecisionVerifier(appConn(t, db), verifier)
	ctx := context.Background()
	base := testCycle(t, "cycle-rejected-roundtrip")
	if err := store.Save(ctx, tenant.String(), base); err != nil {
		t.Fatal(err)
	}
	proposed, rec, err := base.Propose("worker-1", testDecimal(t, "0.05"), "manager-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, tenant.String(), proposed); err != nil {
		t.Fatal(err)
	}
	receipt := merit.RejectionReceipt{ReceiptID: "signed-rejection", ApproverID: "approver-1", RecommendationDigest: rec.CanonicalDigest}
	verifier.receipts[receipt.ReceiptID] = receipt.RecommendationDigest
	rejected, err := proposed.Reject("worker-1", receipt, verifier)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, tenant.String(), rejected); err != nil {
		t.Fatal(err)
	}
	finalized, err := rejected.Finalize()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, tenant.String(), finalized); err != nil {
		t.Fatal(err)
	}
	got, err := meritstore.NewWithDecisionVerifier(appConn(t, db), verifier).Current(ctx, tenant.String(), base.CycleID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Recommendations[0].State != merit.RecommendationRejected || got.Recommendations[0].RejectionReceiptID != receipt.ReceiptID || got.Recommendations[0].RejectedDigest != rec.CanonicalDigest {
		t.Fatalf("rejected decision round trip=%#v", got.Recommendations[0])
	}
	if _, err := meritstore.NewWithDecisionVerifier(appConn(t, db), verifier).Current(ctx, otherTenant.String(), base.CycleID); !errors.Is(err, merit.ErrStoreNotFound) {
		t.Fatalf("cross-tenant decided cycle=%v", err)
	}
	badVerifier := decisionVerifier{receipts: map[string]string{receipt.ReceiptID: "sha256:tampered"}}
	if _, err := meritstore.NewWithDecisionVerifier(appConn(t, db), badVerifier).Current(ctx, tenant.String(), base.CycleID); !errors.Is(err, merit.ErrStoreInvalid) {
		t.Fatalf("tampered decision accepted=%v", err)
	}
}

func TestMeritPopulatedLegacyDecisionOutcomeMigration(t *testing.T) {
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 267); err != nil {
		t.Fatalf("migrate legacy schema: %v", err)
	}
	tenant := insertTenant(t, db)
	base := testCycle(t, "cycle-legacy-decision")
	proposed, rec, err := base.Propose("worker-1", testDecimal(t, "0.05"), "manager-1")
	if err != nil {
		t.Fatal(err)
	}
	populationRow := uuid.New()
	members, _ := json.Marshal([]map[string]string{{
		"participant_id": "worker-1", "manager_id": "manager-1", "base_pay": "100.00 USD",
		"performance_rating": "4.00", "band_position": "0.50", "salary_revision_ref": "salary-1",
		"performance_ref": "performance-1", "effective_at": testInstant().String(), "known_at": testInstant().String(),
	}})
	db.Exec(t, `INSERT INTO merit_population_snapshot
		(tenant_id,row_id,snapshot_id,revision,members,watermark,frozen,frozen_at,canonical_digest)
		VALUES ($1,$2,$3,1,$4,$5,true,$6,$7)`, tenant, populationRow, base.Population.SnapshotID, members,
		base.Population.Watermark.String(), base.Population.FrozenAt.Time(), base.Population.CanonicalDigest[len("sha256:"):])
	guidelines, _ := json.Marshal(map[string]any{
		"matrix": map[string]any{"matrix_id": base.Guidelines.MatrixID, "version": base.Guidelines.Version,
			"rules":            []map[string]string{{"rating_min": "4.00", "rating_max": "4.00", "band_position_min": "0.00", "band_position_max": "1.00", "minimum_rate": "0.05", "maximum_rate": "0.10"}},
			"canonical_digest": base.Guidelines.CanonicalDigest},
		"currency": "USD", "budget": "20.00",
	})
	db.Exec(t, `INSERT INTO merit_cycle_revision
		(tenant_id,row_id,cycle_id,revision,parent_revision,parent_digest,population_ref,guidelines,budget,state,effective_at,known_at,canonical_digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,20,'OPEN',$9,$10,$11)`, tenant, uuid.New(), proposed.CycleID, proposed.Revision,
		proposed.ParentRevision, proposed.ParentDigest[len("sha256:"):], populationRow, guidelines, proposed.EffectiveAt.Time(), proposed.KnownAt.Time(), proposed.CanonicalDigest[len("sha256:"):])
	payload, _ := json.Marshal(map[string]any{
		"cycle_revision": rec.CycleRevision, "base_pay": "100.00 USD", "performance_rating": "4.00", "band_position": "0.50",
		"salary_revision_ref": "salary-1", "performance_ref": "performance-1", "rate": "0.05", "amount": "5.00",
		"guideline_digest": rec.GuidelineDigest, "proposed_by": rec.ProposedBy, "adjustments": []any{},
	})
	digests := map[string]string{"a-approved": meritDigest, "b-adjusted": strings.Repeat("b", 64), "c-proposed": strings.Repeat("c", 64)}
	states := map[string]string{"a-approved": "APPROVED", "b-adjusted": "ADJUSTED", "c-proposed": "PROPOSED"}
	for participant, digest := range digests {
		db.Exec(t, `INSERT INTO merit_recommendation
			(tenant_id,row_id,cycle_id,cycle_revision,participant_id,base_pay,state,adjustments,canonical_digest)
			VALUES ($1,$2,$3,$4,$5,100,$6,$7,$8)`, tenant, uuid.New(), proposed.CycleID, proposed.Revision, participant, states[participant], payload, digest)
	}
	if _, err := db.Provider(t).UpTo(context.Background(), 268); err != nil {
		t.Fatalf("migrate populated legacy schema: %v", err)
	}
	for participant, digest := range digests {
		var gotState, gotDigest string
		var evidence []byte
		if err := db.QueryRow(context.Background(), `SELECT state,canonical_digest,decision_evidence::text FROM merit_recommendation WHERE tenant_id=$1 AND participant_id=$2`, tenant, participant).Scan(&gotState, &gotDigest, &evidence); err != nil {
			t.Fatal(err)
		}
		if gotState != states[participant] || gotDigest != digest || string(evidence) != `{"version": 1}` {
			t.Fatalf("legacy row %s state=%s digest=%s evidence=%s", participant, gotState, gotDigest, evidence)
		}
	}
	if _, err := meritstore.NewWithDecisionVerifier(appConn(t, db), decisionVerifier{receipts: map[string]string{}}).Current(context.Background(), tenant.String(), proposed.CycleID); !errors.Is(err, merit.ErrStoreInvalid) {
		t.Fatalf("unverified legacy approval accepted=%v", err)
	}
}

func TestTodo_PERSIST_MERIT_001_Mutation(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db)
	conn := appConn(t, db)
	cycle := testCycle(t, "cycle-mutation")
	if err := meritstore.New(conn).Save(context.Background(), tenant.String(), cycle); err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`UPDATE merit_population_snapshot SET frozen=false WHERE tenant_id=$1`,
		`DELETE FROM merit_population_snapshot WHERE tenant_id=$1`,
		`UPDATE merit_cycle_revision SET state='OPEN' WHERE tenant_id=$1`,
		`DELETE FROM merit_cycle_revision WHERE tenant_id=$1`,
		`UPDATE merit_recommendation SET state='ADJUSTED' WHERE tenant_id=$1`,
		`DELETE FROM merit_recommendation WHERE tenant_id=$1`,
	}
	for _, statement := range statements {
		err := tenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := tx.Exec(context.Background(), statement, tenant)
			return err
		})
		if err == nil {
			t.Fatalf("mutation accepted: %s", statement)
		}
	}
}

var _ dbport.Beginner = (*pgxadapter.Conn)(nil)
