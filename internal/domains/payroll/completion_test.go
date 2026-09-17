package payroll

import (
	"bytes"
	"errors"
	"sync"
	"testing"
)

func completionReleaseFixture(t *testing.T) PayrollRelease {
	t.Helper()
	release, err := ReleasePayroll(releaseRequestFixture(t), nil)
	if err != nil {
		t.Fatalf("ReleasePayroll: %v", err)
	}
	return release
}

func completionObservationsFixture() []DimensionObservation {
	return []DimensionObservation{
		{Dimension: CompletionEmployeeResults, Status: CompletionConsistent, EvidenceDigest: "sha256:obs-results"},
		{Dimension: CompletionPayments, Status: CompletionConsistent, EvidenceDigest: "sha256:obs-payments"},
		{Dimension: CompletionGeneralLedger, Status: CompletionConsistent, EvidenceDigest: "sha256:obs-gl"},
		{Dimension: CompletionTaxFilings, Status: CompletionConsistent, EvidenceDigest: "sha256:obs-tax"},
		{Dimension: CompletionStatements, Status: CompletionConsistent, EvidenceDigest: "sha256:obs-statements"},
		{Dimension: CompletionExternalObservations, Status: CompletionConsistent, EvidenceDigest: "sha256:obs-external"},
	}
}

func completionStatusByDim(r CompletionReconciliation) map[CompletionDimension]CompletionStatus {
	out := map[CompletionDimension]CompletionStatus{}
	for _, d := range r.Dimensions {
		out[d.Dimension] = d.Status
	}
	return out
}

// TestTodo_PAYRUN_009 is the primary acceptance case: every completion
// dimension reports CONSISTENT|DEGRADED|REPAIR_REQUIRED|UNKNOWN independently,
// and one success can never mask another failure.
func TestTodo_PAYRUN_009(t *testing.T) {
	release := completionReleaseFixture(t)
	obs := completionObservationsFixture()
	obs[1].Status = CompletionRepairRequired
	obs[1].EvidenceDigest = "sha256:obs-payments-shortfall"
	recon, err := ReconcileCompletion(release, obs)
	if err != nil {
		t.Fatalf("ReconcileCompletion: %v", err)
	}
	byDim := completionStatusByDim(recon)
	if byDim[CompletionPayments] != CompletionRepairRequired {
		t.Fatalf("payments = %s, want REPAIR_REQUIRED", byDim[CompletionPayments])
	}
	for dim, want := range map[CompletionDimension]CompletionStatus{
		CompletionEmployeeResults: CompletionConsistent, CompletionGeneralLedger: CompletionConsistent,
		CompletionTaxFilings: CompletionConsistent, CompletionStatements: CompletionConsistent,
		CompletionExternalObservations: CompletionConsistent,
	} {
		if byDim[dim] != want {
			t.Fatalf("%s = %s, want CONSISTENT", dim, byDim[dim])
		}
	}
	if recon.Overall != CompletionRepairRequired {
		t.Fatalf("overall = %s, want REPAIR_REQUIRED: one success masked a failure", recon.Overall)
	}
	if recon.ReleaseDigest != release.ReleaseDigest || recon.RunID != release.RunID || recon.RunRevision != release.RunRevision {
		t.Fatalf("reconciliation lost release identity: %+v", recon)
	}
	if recon.ReconciliationID == "" || recon.ReconciliationDigest == "" {
		t.Fatal("reconciliation identity is incomplete")
	}
	if err := recon.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	explanation, err := recon.Explain()
	if err != nil || explanation.Overall != CompletionRepairRequired || explanation.Digest != recon.ReconciliationDigest {
		t.Fatalf("explanation = %+v, err = %v", explanation, err)
	}
	// A missing observation is UNKNOWN, never assumed consistent.
	partial := completionObservationsFixture()[:5]
	partialRecon, err := ReconcileCompletion(release, partial)
	if err != nil {
		t.Fatalf("partial ReconcileCompletion: %v", err)
	}
	if completionStatusByDim(partialRecon)[CompletionExternalObservations] != CompletionUnknown {
		t.Fatalf("missing dimension was not UNKNOWN: %+v", partialRecon.Dimensions)
	}
	if partialRecon.Overall != CompletionUnknown {
		t.Fatalf("partial overall = %s, want UNKNOWN", partialRecon.Overall)
	}
}

// TestTodo_PAYRUN_009_Property proves the severity algebra: REPAIR_REQUIRED
// dominates UNKNOWN dominates DEGRADED dominates CONSISTENT, and every
// malformed observation is refused.
func TestTodo_PAYRUN_009_Property(t *testing.T) {
	release := completionReleaseFixture(t)
	cases := []struct {
		name    string
		first   CompletionStatus
		second  CompletionStatus
		overall CompletionStatus
	}{
		{"repair dominates unknown", CompletionRepairRequired, CompletionUnknown, CompletionRepairRequired},
		{"unknown dominates degraded", CompletionUnknown, CompletionDegraded, CompletionUnknown},
		{"degraded dominates consistent", CompletionDegraded, CompletionConsistent, CompletionDegraded},
	}
	for _, tc := range cases {
		obs := completionObservationsFixture()
		obs[0].Status = tc.first
		obs[1].Status = tc.second
		for i := range obs {
			if obs[i].Status == CompletionUnknown {
				obs[i].EvidenceDigest = ""
			} else {
				obs[i].EvidenceDigest = "sha256:obs-" + string(obs[i].Dimension)
			}
		}
		recon, err := ReconcileCompletion(release, obs)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if recon.Overall != tc.overall {
			t.Fatalf("%s overall = %s, want %s", tc.name, recon.Overall, tc.overall)
		}
	}
	allConsistent, err := ReconcileCompletion(release, completionObservationsFixture())
	if err != nil {
		t.Fatal(err)
	}
	if allConsistent.Overall != CompletionConsistent {
		t.Fatalf("all-consistent overall = %s", allConsistent.Overall)
	}
	bad := [][]DimensionObservation{
		append(completionObservationsFixture(), completionObservationsFixture()[0]),
		{{Dimension: "WISHFUL", Status: CompletionConsistent, EvidenceDigest: "sha256:x"}},
		{{Dimension: CompletionPayments, Status: "SPOTLESS", EvidenceDigest: "sha256:x"}},
		{{Dimension: CompletionPayments, Status: CompletionConsistent, EvidenceDigest: ""}},
	}
	for i, obs := range bad {
		if _, err := ReconcileCompletion(release, obs); !errors.Is(err, ErrCompletionRejected) {
			t.Fatalf("bad case %d error = %v", i, err)
		}
	}
}

// TestTodo_PAYRUN_009_Golden pins byte-identical completion evidence.
func TestTodo_PAYRUN_009_Golden(t *testing.T) {
	release := completionReleaseFixture(t)
	first, err := ReconcileCompletion(release, completionObservationsFixture())
	if err != nil {
		t.Fatal(err)
	}
	second, err := ReconcileCompletion(release, completionObservationsFixture())
	if err != nil {
		t.Fatal(err)
	}
	if first.ReconciliationDigest != second.ReconciliationDigest || !bytes.Equal(first.Canonical(), second.Canonical()) {
		t.Fatal("identical completion inputs did not produce identical evidence")
	}
	digest, err := second.Digest()
	if err != nil || digest != second.ReconciliationDigest {
		t.Fatalf("Digest() = %q, err = %v", digest, err)
	}
}

// TestTodo_PAYRUN_009_Race proves concurrent reconciliations agree exactly.
func TestTodo_PAYRUN_009_Race(t *testing.T) {
	release := completionReleaseFixture(t)
	obs := completionObservationsFixture()
	var wg sync.WaitGroup
	digests := make([]string, 8)
	errs := make([]error, 8)
	for i := range digests {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			recon, err := ReconcileCompletion(release, obs)
			if err != nil {
				errs[i] = err
				return
			}
			digests[i] = recon.ReconciliationDigest
			_ = recon.Validate()
			_ = recon.Canonical()
			_, _ = recon.Explain()
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
		if digests[i] != digests[0] {
			t.Fatal("concurrent reconciliations diverged")
		}
	}
}

// TestTodo_PAYRUN_009_Integration proves the reconciliation closes the
// lifecycle opened by the release: it binds the exact released revision while
// the run itself remains settleable.
func TestTodo_PAYRUN_009_Integration(t *testing.T) {
	req := releaseRequestFixture(t)
	release, err := ReleasePayroll(req, nil)
	if err != nil {
		t.Fatal(err)
	}
	recon, err := ReconcileCompletion(release, completionObservationsFixture())
	if err != nil {
		t.Fatal(err)
	}
	if recon.RunRevision != req.Run.Revision || recon.ReleaseDigest != release.ReleaseDigest {
		t.Fatalf("reconciliation does not bind the released revision: %+v", recon)
	}
	settled, err := req.Run.Settle()
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if settled.State != Settled {
		t.Fatalf("settled state = %s", settled.State)
	}
}

// TestTodo_PAYRUN_009_Fault proves reconciliation over a forged release is
// refused with a typed error and yields no reconciliation.
func TestTodo_PAYRUN_009_Fault(t *testing.T) {
	release := completionReleaseFixture(t)
	forged := release
	forged.ReleaseDigest = "sha256:forged"
	leaked, err := ReconcileCompletion(forged, completionObservationsFixture())
	if !errors.Is(err, ErrCompletionRejected) {
		t.Fatalf("forged release error = %v", err)
	}
	if leaked.ReconciliationDigest != "" || len(leaked.Dimensions) != 0 {
		t.Fatalf("forged release leaked a reconciliation: %+v", leaked)
	}
	empty := PayrollRelease{}
	if _, err := ReconcileCompletion(empty, completionObservationsFixture()); !errors.Is(err, ErrCompletionRejected) {
		t.Fatalf("empty release error = %v", err)
	}
	var refusal *CompletionError
	if _, err := ReconcileCompletion(forged, completionObservationsFixture()); !errors.As(err, &refusal) || refusal.Field == "" || refusal.Reason == "" {
		t.Fatalf("refusal = %+v, want field and reason", err)
	}
}

// TestTodo_PAYRUN_009_Mutation proves the reconciliation digest binds every
// dimension: any status or evidence change yields a new digest and a tampered
// reconciliation fails validation.
func TestTodo_PAYRUN_009_Mutation(t *testing.T) {
	release := completionReleaseFixture(t)
	base, err := ReconcileCompletion(release, completionObservationsFixture())
	if err != nil {
		t.Fatal(err)
	}
	changed := completionObservationsFixture()
	changed[2].Status = CompletionDegraded
	changed[2].EvidenceDigest = "sha256:obs-gl-variance"
	mutated, err := ReconcileCompletion(release, changed)
	if err != nil {
		t.Fatal(err)
	}
	if mutated.ReconciliationDigest == base.ReconciliationDigest {
		t.Fatal("dimension change did not change the digest")
	}
	if mutated.Overall != CompletionDegraded {
		t.Fatalf("overall = %s, want DEGRADED", mutated.Overall)
	}
	tampered := base
	tampered.Dimensions[0].Status = CompletionRepairRequired
	if err := tampered.Validate(); err == nil {
		t.Fatal("tampered reconciliation passed validation")
	}
	forged := base
	forged.ReconciliationDigest = "sha256:forged"
	if err := forged.Validate(); err == nil {
		t.Fatal("forged digest passed validation")
	}
}
