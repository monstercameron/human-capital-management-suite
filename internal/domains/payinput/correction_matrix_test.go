package payinput

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_PAYINPUT_003_Property: correction and snapshot outputs are
// deterministic and use exact decimal math — never float.
func TestTodo_PAYINPUT_003_Property(t *testing.T) {
	_, bound, _, ledger := correctionFixture(t)
	request := correctionRequest(t, bound)

	first, err := PlanCorrection(ledger, request, correctionClock())
	if err != nil {
		t.Fatal(err)
	}
	second, err := PlanCorrection(ledger, request, correctionClock())
	if err != nil {
		t.Fatal(err)
	}
	if first.CanonicalDigest != second.CanonicalDigest {
		t.Fatal("identical correction inputs replayed to different digests")
	}

	// Exact decimal rules: 0.30 - 0.10 is exactly 0.20.
	exact := request
	exact.PriorAmount = payInputDecimal(t, "0.10")
	exact.CorrectedAmount = payInputDecimal(t, "0.30")
	plan, err := PlanCorrection(ledger, exact, correctionClock())
	if err != nil {
		t.Fatal(err)
	}
	if plan.TotalDelta.String() != "0.20" {
		t.Fatalf("inexact decimal delta = %q", plan.TotalDelta.String())
	}

	snapshot, err := FreezeSnapshot("snap-p", "2026-01", snapshotInputs(t), correctionClock())
	if err != nil {
		t.Fatal(err)
	}
	again, err := FreezeSnapshot("snap-p", "2026-01", snapshotInputs(t), correctionClock())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.CanonicalDigest != again.CanonicalDigest {
		t.Fatal("identical snapshot inputs replayed to different digests")
	}
}

// TestTodo_PAYINPUT_003_Golden: digests are canonical sha256 bytes and the
// snapshot records every revision, presence flag and watermark.
func TestTodo_PAYINPUT_003_Golden(t *testing.T) {
	_, bound, _, ledger := correctionFixture(t)
	plan, err := PlanCorrection(ledger, correctionRequest(t, bound), correctionClock())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(plan.CanonicalDigest, "sha256:") || plan.Canonical() == nil {
		t.Fatalf("correction plan has no canonical digest: %+v", plan)
	}
	snapshot, err := FreezeSnapshot("snap-g", "2026-01", snapshotInputs(t), correctionClock())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(snapshot.CanonicalDigest, "sha256:") || snapshot.Canonical() == nil {
		t.Fatalf("snapshot has no canonical digest: %+v", snapshot)
	}
	if len(snapshot.Inputs) != 4 {
		t.Fatalf("snapshot records %d inputs, want all four sources", len(snapshot.Inputs))
	}
}

// TestTodo_PAYINPUT_003_Race: concurrent corrections and freezes share no
// mutable state.
func TestTodo_PAYINPUT_003_Race(t *testing.T) {
	_, bound, _, ledger := correctionFixture(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := PlanCorrection(ledger, correctionRequest(t, bound), correctionClock()); err != nil {
				t.Error(err)
			}
			if _, err := FreezeSnapshot("snap-r", "2026-01", snapshotInputs(t), correctionClock()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}

// TestTodo_PAYINPUT_003_Fault: empty ledgers, unknown digests and unknown
// periods refuse with typed errors and no plan.
func TestTodo_PAYINPUT_003_Fault(t *testing.T) {
	_, bound, _, ledger := correctionFixture(t)
	request := correctionRequest(t, bound)

	if _, err := PlanCorrection(nil, request, correctionClock()); !errors.Is(err, ErrCorrectionRejected) {
		t.Fatalf("empty ledger error = %v", err)
	}
	unknown := request
	unknown.AssignmentDigest = "sha256:" + strings.Repeat("0", 64)
	if _, err := PlanCorrection(ledger, unknown, correctionClock()); !errors.Is(err, ErrCorrectionRejected) {
		t.Fatalf("unknown digest error = %v", err)
	}
	badPeriod := request
	badPeriod.DependentPeriods = []string{"2026-01", "2099-12"}
	if _, err := PlanCorrection(ledger, badPeriod, correctionClock()); !errors.Is(err, ErrCorrectionRejected) {
		t.Fatalf("unknown period error = %v", err)
	}
	if _, err := FreezeSnapshot("snap-f", "2026-01", nil, correctionClock()); !errors.Is(err, ErrCorrectionRejected) {
		t.Fatalf("nil snapshot inputs error = %v", err)
	}
}

// TestTodo_PAYINPUT_003_Security: explanations carry policy references and
// digests only — never worker identities.
func TestTodo_PAYINPUT_003_Security(t *testing.T) {
	_, bound, _, ledger := correctionFixture(t)
	plan, err := PlanCorrection(ledger, correctionRequest(t, bound), correctionClock())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plan.Explain(), "worker-1") {
		t.Fatalf("correction explanation leaks a worker identity: %q", plan.Explain())
	}
	snapshot, err := FreezeSnapshot("snap-s", "2026-01", snapshotInputs(t), correctionClock())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(snapshot.Explain(), "worker-1") {
		t.Fatalf("snapshot explanation leaks a worker identity: %q", snapshot.Explain())
	}
}

// TestTodo_PAYINPUT_003_Conformance: the plan binds the exact superseded
// revision and every affected run; the snapshot covers all four sources.
func TestTodo_PAYINPUT_003_Conformance(t *testing.T) {
	_, bound, _, ledger := correctionFixture(t)
	plan, err := PlanCorrection(ledger, correctionRequest(t, bound), correctionClock())
	if err != nil {
		t.Fatal(err)
	}
	if plan.CorrectionID != "corr-1" || plan.SupersedesDigest != bound.CanonicalDigest {
		t.Fatalf("plan does not bind the exact correction: %+v", plan)
	}
	snapshot, err := FreezeSnapshot("snap-c", "2026-01", snapshotInputs(t), correctionClock())
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []InputSource{SourceTime, SourceBenefit, SourceTax, SourceGarnishment} {
		found := false
		for _, input := range snapshot.Inputs {
			if input.Source == source && input.Present && input.Digest != "" && input.Watermark.IsSet() {
				found = true
			}
		}
		if !found {
			t.Fatalf("snapshot misses complete source %s", source)
		}
	}
	if err := snapshot.Effective(); err != nil {
		t.Fatalf("frozen snapshot is not effective: %v", err)
	}
}

// TestTodo_PAYINPUT_003_Mutation: seeded mutants die — a dropped source, a
// backdated cutoff and a rewritten finalized run all refuse.
func TestTodo_PAYINPUT_003_Mutation(t *testing.T) {
	_, bound, _, ledger := correctionFixture(t)

	dropped := snapshotInputs(t)[:3]
	if _, err := FreezeSnapshot("snap-m", "2026-01", dropped, correctionClock()); !errors.Is(err, ErrCorrectionRejected) {
		t.Fatalf("dropped source error = %v", err)
	}

	early := values.NewInstant(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	inputs := snapshotInputs(t)
	late := append([]SnapshotInput(nil), inputs...)
	late[0].Watermark = values.NewInstant(time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC))
	if _, err := FreezeSnapshot("snap-m", "2026-01", late, early); !errors.Is(err, ErrCorrectionRejected) {
		t.Fatalf("watermark after cutoff error = %v", err)
	}

	// A finalized run can only be named by a plan, never rewritten: the
	// request must name the exact superseded digest it corrects.
	blank := correctionRequest(t, bound)
	blank.AssignmentDigest = ""
	if _, err := PlanCorrection(ledger, blank, correctionClock()); !errors.Is(err, ErrCorrectionRejected) {
		t.Fatalf("blank superseded digest error = %v", err)
	}
}
