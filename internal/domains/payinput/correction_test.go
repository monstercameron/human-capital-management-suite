package payinput

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func correctionClock() values.Instant {
	return values.NewInstant(time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))
}

// correctionFixture binds two assignments and records them in one finalized
// run plus one open run. The finalized run must never be rewritten.
func correctionFixture(t *testing.T) (Definition, WorkerAssignment, WorkerAssignment, []PayRun) {
	t.Helper()
	definition := validDefinition(t, StatePublished)
	first := validAssignment(t, "a-1", "worker-1", "2026-01-01", "2026-04-01")
	first, err := NewWorkerAssignment(definition, first)
	if err != nil {
		t.Fatal(err)
	}
	second := validAssignment(t, "a-2", "worker-1", "2026-01-01", "2026-04-01")
	second, err = NewWorkerAssignment(definition, second)
	if err != nil {
		t.Fatal(err)
	}
	finalized, err := NewPayRun(PayRun{RunID: "run-2026-01", PeriodID: "2026-01", State: PayRunFinalized, AssignmentDigests: []string{first.CanonicalDigest}})
	if err != nil {
		t.Fatal(err)
	}
	open, err := NewPayRun(PayRun{RunID: "run-2026-02", PeriodID: "2026-02", State: PayRunOpen, AssignmentDigests: []string{first.CanonicalDigest, second.CanonicalDigest}})
	if err != nil {
		t.Fatal(err)
	}
	return definition, first, second, []PayRun{finalized, open}
}

func correctionRequest(t *testing.T, bound WorkerAssignment) CorrectionRequest {
	t.Helper()
	return CorrectionRequest{
		CorrectionID:     "corr-1",
		AssignmentDigest: bound.CanonicalDigest,
		DependentPeriods: []string{"2026-01", "2026-02"},
		PriorAmount:      payInputDecimal(t, "25.00"),
		CorrectedAmount:  payInputDecimal(t, "27.50"),
		Reason:           "retro rate adjustment",
		RequestedAt:      correctionClock(),
	}
}

func snapshotInputs(t *testing.T) []SnapshotInput {
	t.Helper()
	watermark := values.NewInstant(time.Date(2026, 2, 28, 23, 0, 0, 0, time.UTC))
	return []SnapshotInput{
		{Source: SourceTime, Revision: "time-r1", Digest: "sha256:time", Present: true, Watermark: watermark},
		{Source: SourceBenefit, Revision: "ben-r1", Digest: "sha256:benefit", Present: true, Watermark: watermark},
		{Source: SourceTax, Revision: "tax-r1", Digest: "sha256:tax", Present: true, Watermark: watermark},
		{Source: SourceGarnishment, Revision: "gar-r1", Digest: "sha256:garnishment", Present: true, Watermark: watermark},
	}
}

// TestTodo_PAYINPUT_003: an append-only correction emits the affected-run
// plan without rewriting a finalized run, and the frozen snapshot records
// every input revision, presence flag and watermark behind a cutoff.
func TestTodo_PAYINPUT_003(t *testing.T) {
	_, bound, _, ledger := correctionFixture(t)
	ledgerBefore := append([]PayRun(nil), ledger...)
	request := correctionRequest(t, bound)

	plan, err := PlanCorrection(ledger, request, correctionClock())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.AffectedRuns) != 2 || plan.AffectedRuns[0] != "run-2026-01" || plan.AffectedRuns[1] != "run-2026-02" {
		t.Fatalf("affected runs = %v", plan.AffectedRuns)
	}
	if plan.TotalDelta.String() != "2.50" {
		t.Fatalf("total delta = %q, want exact 2.50", plan.TotalDelta.String())
	}
	if plan.SupersedesDigest != bound.CanonicalDigest || plan.CanonicalDigest == "" {
		t.Fatalf("correction plan does not bind the superseded revision: %+v", plan)
	}

	// The finalized run was planned, never rewritten in place.
	if !reflect.DeepEqual(ledger, ledgerBefore) {
		t.Fatal("correction mutated the finalized run ledger")
	}

	// A correction that omits a dependent period is refused.
	omitted := request
	omitted.DependentPeriods = []string{"2026-01"}
	if _, err := PlanCorrection(ledger, omitted, correctionClock()); !errors.Is(err, ErrCorrectionRejected) {
		t.Fatalf("omitted dependent period error = %v", err)
	}

	// The frozen snapshot is complete and immutable after its cutoff.
	snapshot, err := FreezeSnapshot("snap-2026-01", "2026-01", snapshotInputs(t), correctionClock())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.CanonicalDigest == "" || snapshot.Cutoff != correctionClock() {
		t.Fatalf("snapshot is not frozen behind its cutoff: %+v", snapshot)
	}
	if err := VerifySnapshot(snapshot, snapshotInputs(t), correctionClock()); err != nil {
		t.Fatalf("unchanged snapshot failed verification: %v", err)
	}
	drifted := snapshotInputs(t)
	drifted[2].Digest = "sha256:changed-after-cutoff"
	if err := VerifySnapshot(snapshot, drifted, correctionClock()); !errors.Is(err, ErrSnapshotFrozen) {
		t.Fatalf("post-cutoff change error = %v", err)
	}
}

// TestPayrollInputCorrectionPreservesClosedRunsAndFreezesCompleteManifest
// verifies corrections cannot target an assignment outside the ledger.
func TestPayrollInputCorrectionPreservesClosedRunsAndFreezesCompleteManifest(t *testing.T) {
	_, bound, _, ledger := correctionFixture(t)
	request := correctionRequest(t, bound)
	request.AssignmentDigest = "sha256:unknown-assignment"
	_, err := PlanCorrection(ledger, request, correctionClock())
	var correctionErr *CorrectionError
	if !errors.Is(err, ErrCorrectionRejected) || !errors.As(err, &correctionErr) {
		t.Fatalf("unknown assignment error = %v", err)
	}
	if correctionErr.Field != "assignment_digest" || correctionErr.State != "unknown" {
		t.Fatalf("unknown assignment refusal = %+v", correctionErr)
	}
}
