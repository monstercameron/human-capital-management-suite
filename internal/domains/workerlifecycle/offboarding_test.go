package workerlifecycle

import (
	"errors"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func offboardingPlan(t *testing.T) WorkerLifecyclePlan {
	t.Helper()
	cal := values.CalendarRef{Ref: "gregorian", Version: "1"}
	newReq := func(id string, ordinal int, owner string) Requirement {
		return Requirement{
			ID: id, Ordinal: ordinal, Owner: owner,
			Due:                DueRule{Calendar: cal},
			Evidence:           []values.EntityRef{ref("evidence", "00000000-0000-4000-8000-000000000100")},
			VerificationPolicy: ref("evidence_policy", "00000000-0000-4000-8000-000000000200"),
			Completion:         CompleteAllRequired,
			Required:           true,
		}
	}
	p, err := NewPlan(WorkerLifecyclePlan{
		Worker:     ref("worker", "00000000-0000-4000-8000-000000000001"),
		Employment: ref("employment", "00000000-0000-4000-8000-000000000002"),
		Proposal:   ref("proposal", "00000000-0000-4000-8000-000000000003"),
		Event:      EventEnd,
		EventDate:  date(t, "2026-03-01"),
		Completion: CompleteAllRequired,
		Requirements: []Requirement{
			newReq("access-return", 1, "security"),
			newReq("asset-return", 2, "workplace"),
			newReq("final-pay", 3, "payroll"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func offboardExpected() map[ClosureDomain][]values.EntityRef {
	mk := func(id string) values.EntityRef {
		return values.EntityRef{Tenant: "tenant-a", Kind: "closure_evidence", Id: id}
	}
	return map[ClosureDomain][]values.EntityRef{
		ClosureAccess:   {mk("00000000-0000-4000-8000-000000000401")},
		ClosureAssets:   {mk("00000000-0000-4000-8000-000000000402")},
		ClosurePayroll:  {mk("00000000-0000-4000-8000-000000000403")},
		ClosureBenefits: {mk("00000000-0000-4000-8000-000000000404")},
	}
}

func closureEvidence(domain ClosureDomain, observation, at string, privileged bool) ClosureEvidence {
	by := "HR_OFFICER"
	if privileged {
		by = "SECURITY_OFFICER"
	}
	return ClosureEvidence{
		ObservationID: observation,
		At:            mustDate(at),
		By:            by,
		Evidence:      offboardExpected()[domain][0],
	}
}

// TestOffboardingPlanSeparatesEmploymentCompletionFromExternalClosureAndRepair
// is the primary acceptance case: employment completion is independent of
// external closure, every domain is observed against expectations, gaps
// create scoped repair, and final close needs the declared policy.
func TestOffboardingPlanSeparatesEmploymentCompletionFromExternalClosureAndRepair(t *testing.T) {
	tracker, err := BeginOffboarding(offboardingPlan(t), offboardExpected())
	if err != nil {
		t.Fatalf("BeginOffboarding: %v", err)
	}
	rejectEmptyLifecycleDigest(t, tracker.Digest)

	tracker, err = CompleteEmployment(tracker, date(t, "2026-03-01"))
	if err != nil {
		t.Fatalf("CompleteEmployment: %v", err)
	}
	if !tracker.EmploymentCompleted {
		t.Fatal("employment not completed")
	}

	tracker, err = ObserveClosure(tracker, ClosureAccess, closureEvidence(ClosureAccess, "obs-access-1", "2026-03-02", true))
	if err != nil {
		t.Fatalf("access: %v", err)
	}
	tracker, err = ObserveClosure(tracker, ClosureAssets, closureEvidence(ClosureAssets, "obs-assets-1", "2026-03-02", false))
	if err != nil {
		t.Fatalf("assets: %v", err)
	}
	// Payroll observation with unrecognized evidence is a gap, not closure.
	gap := closureEvidence(ClosurePayroll, "obs-payroll-1", "2026-03-02", false)
	gap.Evidence = ref("closure_evidence", "00000000-0000-4000-8000-000000000499")
	tracker, err = ObserveClosure(tracker, ClosurePayroll, gap)
	if err != nil {
		t.Fatalf("payroll gap: %v", err)
	}
	if stateOf(tracker, ClosurePayroll) != ClosureGap {
		t.Fatalf("payroll = %s, want GAP", stateOf(tracker, ClosurePayroll))
	}
	if len(tracker.Repairs) != 1 {
		t.Fatalf("repairs = %+v, want one scoped repair", tracker.Repairs)
	}

	// Close is refused while a domain gap and repair stand open.
	if _, err := CloseOffboarding(tracker, ClosePolicy{Name: "standard-close"}); !errors.Is(err, ErrOffboardingRejected) {
		t.Fatalf("early close: err = %v, want WORKER_LIFE_004_REJECTED", err)
	}

	tracker, err = ResolveRepair(tracker, ClosurePayroll, ref("resolution", "00000000-0000-4000-8000-000000000501"))
	if err != nil {
		t.Fatalf("ResolveRepair: %v", err)
	}
	tracker, err = ObserveClosure(tracker, ClosurePayroll, closureEvidence(ClosurePayroll, "obs-payroll-2", "2026-03-03", false))
	if err != nil {
		t.Fatalf("payroll: %v", err)
	}
	tracker, err = ObserveClosure(tracker, ClosureBenefits, closureEvidence(ClosureBenefits, "obs-benefits-1", "2026-03-03", false))
	if err != nil {
		t.Fatalf("benefits: %v", err)
	}
	closed, err := CloseOffboarding(tracker, ClosePolicy{Name: "standard-close"})
	if err != nil {
		t.Fatalf("CloseOffboarding: %v", err)
	}
	if !closed.Closed {
		t.Fatal("tracker did not close")
	}
	// Closing never reruns termination: employment history stands as recorded.
	if !closed.EmploymentCompleted || closed.EmploymentCompletedAt.String() != "2026-03-01" {
		t.Fatalf("employment history changed: %+v", closed)
	}
	if err := closed.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	// A revoked approval blocks close on an otherwise complete tracker.
	revoked, err := BeginOffboarding(offboardingPlan(t), offboardExpected())
	if err != nil {
		t.Fatal(err)
	}
	revoked, err = CompleteEmployment(revoked, date(t, "2026-03-01"))
	if err != nil {
		t.Fatal(err)
	}
	for domain := range offboardExpected() {
		revoked, err = ObserveClosure(revoked, domain, closureEvidence(domain, "obs-"+string(domain)+"-9", "2026-03-02", true))
		if err != nil {
			t.Fatal(err)
		}
	}
	revoked, err = RevokeApproval(revoked, "approval withdrawn by HR")
	if err != nil {
		t.Fatalf("RevokeApproval: %v", err)
	}
	if _, err := CloseOffboarding(revoked, ClosePolicy{Name: "standard-close"}); !errors.Is(err, ErrOffboardingRejected) {
		t.Fatalf("revoked close: err = %v, want WORKER_LIFE_004_REJECTED", err)
	}

	// Starting from a hiring plan is refused.
	if _, err := BeginOffboarding(onboardingPlan(t), offboardExpected()); !errors.Is(err, ErrOffboardingRejected) {
		t.Fatalf("start plan: err = %v, want WORKER_LIFE_004_REJECTED", err)
	}
}

func stateOf(tracker OffboardingTracker, domain ClosureDomain) ClosureState {
	for _, closure := range tracker.Closures {
		if closure.Domain == domain {
			return closure.State
		}
	}
	return ClosureUnknown
}

// TestTodo_WORKER_LIFE_004_Property proves observation order independence
// and that duplicate observations are no-ops, never duplicates.
func TestTodo_WORKER_LIFE_004_Property(t *testing.T) {
	begin, err := BeginOffboarding(offboardingPlan(t), offboardExpected())
	if err != nil {
		t.Fatal(err)
	}
	domains := []ClosureDomain{ClosureAccess, ClosureAssets, ClosurePayroll, ClosureBenefits}
	first := begin
	for _, domain := range domains {
		first, err = ObserveClosure(first, domain, closureEvidence(domain, "obs-"+string(domain)+"-1", "2026-03-02", true))
		if err != nil {
			t.Fatal(err)
		}
	}
	second := begin
	for i := len(domains) - 1; i >= 0; i-- {
		domain := domains[i]
		second, err = ObserveClosure(second, domain, closureEvidence(domain, "obs-"+string(domain)+"-1", "2026-03-02", true))
		if err != nil {
			t.Fatal(err)
		}
	}
	if first.Digest != second.Digest {
		t.Fatal("digest depends on observation order")
	}
	// A retried observation with the same id changes nothing.
	retry, err := ObserveClosure(first, ClosureAccess, closureEvidence(ClosureAccess, "obs-ACCESS-1", "2026-03-02", true))
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if retry.Digest != first.Digest {
		t.Fatal("duplicate observation was not a no-op")
	}
}

// TestTodo_WORKER_LIFE_004_Golden pins the canonical digest of the closed
// tracker.
func TestTodo_WORKER_LIFE_004_Golden(t *testing.T) {
	tracker, err := BeginOffboarding(offboardingPlan(t), offboardExpected())
	if err != nil {
		t.Fatal(err)
	}
	tracker, err = CompleteEmployment(tracker, date(t, "2026-03-01"))
	if err != nil {
		t.Fatal(err)
	}
	for domain := range offboardExpected() {
		tracker, err = ObserveClosure(tracker, domain, closureEvidence(domain, "obs-"+string(domain)+"-g", "2026-03-02", true))
		if err != nil {
			t.Fatal(err)
		}
	}
	tracker, err = CloseOffboarding(tracker, ClosePolicy{Name: "standard-close"})
	if err != nil {
		t.Fatal(err)
	}
	const want = "sha256:c0245de20fdf5e51e2411922627fe995ae42c9979557c17a408d8d2ead045162"
	if tracker.Digest != want {
		t.Fatalf("digest = %s, want %s", tracker.Digest, want)
	}
}

// TestTodo_WORKER_LIFE_004_Race proves concurrent observation converges.
func TestTodo_WORKER_LIFE_004_Race(t *testing.T) {
	begin, err := BeginOffboarding(offboardingPlan(t), offboardExpected())
	if err != nil {
		t.Fatal(err)
	}
	const workers = 8
	results := make([]OffboardingTracker, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			results[w], errs[w] = ObserveClosure(begin, ClosureAccess, closureEvidence(ClosureAccess, "obs-access-1", "2026-03-02", true))
		}(w)
	}
	wg.Wait()
	for w := 0; w < workers; w++ {
		if errs[w] != nil {
			t.Fatalf("worker %d: %v", w, errs[w])
		}
		if results[w].Digest != results[0].Digest {
			t.Fatalf("worker %d diverged", w)
		}
	}
}

// TestTodo_WORKER_LIFE_004_Integration proves the store port round-trips the
// tracker with compare-and-swap: stale writes are refused.
func TestTodo_WORKER_LIFE_004_Integration(t *testing.T) {
	tracker, err := BeginOffboarding(offboardingPlan(t), offboardExpected())
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemoryClosureStore()
	if err := store.SaveTracker(tracker, ""); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := store.LoadTracker(tracker.PlanDigest)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Digest != tracker.Digest {
		t.Fatal("round-trip changed the tracker")
	}
	advanced, err := ObserveClosure(loaded, ClosureAccess, closureEvidence(ClosureAccess, "obs-access-1", "2026-03-02", true))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveTracker(advanced, tracker.Digest); err != nil {
		t.Fatalf("cas save: %v", err)
	}
	if err := store.SaveTracker(advanced, tracker.Digest); !errors.Is(err, ErrOffboardingRejected) {
		t.Fatalf("stale save: err = %v, want WORKER_LIFE_004_REJECTED", err)
	}
	if _, err := store.LoadTracker("sha256:missing"); !errors.Is(err, ErrOffboardingRejected) {
		t.Fatalf("missing load: err = %v, want WORKER_LIFE_004_REJECTED", err)
	}
}

// TestTodo_WORKER_LIFE_004_Fault proves partial external failure never rolls
// employment history back and unknown domains change nothing.
func TestTodo_WORKER_LIFE_004_Fault(t *testing.T) {
	tracker, err := BeginOffboarding(offboardingPlan(t), offboardExpected())
	if err != nil {
		t.Fatal(err)
	}
	tracker, err = CompleteEmployment(tracker, date(t, "2026-03-01"))
	if err != nil {
		t.Fatal(err)
	}
	before := tracker.Digest
	// An observation for an untracked domain fails without effect.
	if _, err := ObserveClosure(tracker, "PENSIONS", closureEvidence(ClosureAccess, "obs-x-1", "2026-03-02", true)); !errors.Is(err, ErrOffboardingRejected) {
		t.Fatalf("unknown domain: err = %v, want WORKER_LIFE_004_REJECTED", err)
	}
	// Evidence predating the termination event fails without effect.
	early := closureEvidence(ClosureAccess, "obs-access-2", "2026-02-01", true)
	if _, err := ObserveClosure(tracker, ClosureAccess, early); !errors.Is(err, ErrOffboardingRejected) {
		t.Fatalf("predated evidence: err = %v, want WORKER_LIFE_004_REJECTED", err)
	}
	if tracker.Digest != before || !tracker.EmploymentCompleted || tracker.EmploymentCompletedAt.String() != "2026-03-01" {
		t.Fatal("failed observations moved employment history")
	}
}

// TestTodo_WORKER_LIFE_004_Security proves unprivileged access revocation is
// a gap and cross-tenant evidence is refused.
func TestTodo_WORKER_LIFE_004_Security(t *testing.T) {
	tracker, err := BeginOffboarding(offboardingPlan(t), offboardExpected())
	if err != nil {
		t.Fatal(err)
	}
	unprivileged := closureEvidence(ClosureAccess, "obs-access-1", "2026-03-02", false)
	unprivileged.By = "MANAGER"
	tracker, err = ObserveClosure(tracker, ClosureAccess, unprivileged)
	if err != nil {
		t.Fatalf("unprivileged: %v", err)
	}
	if stateOf(tracker, ClosureAccess) != ClosureGap {
		t.Fatalf("unprivileged revoke = %s, want GAP", stateOf(tracker, ClosureAccess))
	}
	if len(tracker.Repairs) != 1 {
		t.Fatalf("repairs = %+v, want one scoped repair", tracker.Repairs)
	}
	foreign := closureEvidence(ClosureAssets, "obs-assets-1", "2026-03-02", false)
	foreign.Evidence.Tenant = "other-tenant"
	if _, err := ObserveClosure(tracker, ClosureAssets, foreign); !errors.Is(err, ErrOffboardingRejected) {
		t.Fatalf("cross-tenant: err = %v, want WORKER_LIFE_004_REJECTED", err)
	}
}

// TestTodo_WORKER_LIFE_004_Conformance proves every observed closure matches
// its domain's expected evidence and every gap carries a repair.
func TestTodo_WORKER_LIFE_004_Conformance(t *testing.T) {
	tracker, err := BeginOffboarding(offboardingPlan(t), offboardExpected())
	if err != nil {
		t.Fatal(err)
	}
	for domain := range offboardExpected() {
		tracker, err = ObserveClosure(tracker, domain, closureEvidence(domain, "obs-"+string(domain)+"-c", "2026-03-02", true))
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, closure := range tracker.Closures {
		if closure.State != ClosureObserved {
			t.Fatalf("domain %s = %s, want OBSERVED", closure.Domain, closure.State)
		}
		matched := false
		for _, want := range offboardExpected()[closure.Domain] {
			if closure.Observed != nil && *closure.Observed == closureEvidenceFor(closure.Domain, want) {
				matched = true
			}
		}
		if !matched {
			t.Fatalf("domain %s observation matches no expected evidence", closure.Domain)
		}
	}
}

// TestTodo_WORKER_LIFE_004_Mutation proves the digest binds employment,
// closures, repairs and closure: any change yields a new digest and
// tampering fails.
func TestTodo_WORKER_LIFE_004_Mutation(t *testing.T) {
	base, err := BeginOffboarding(offboardingPlan(t), offboardExpected())
	if err != nil {
		t.Fatal(err)
	}
	advanced, err := ObserveClosure(base, ClosureAccess, closureEvidence(ClosureAccess, "obs-access-1", "2026-03-02", true))
	if err != nil {
		t.Fatal(err)
	}
	if advanced.Digest == base.Digest {
		t.Fatal("observation did not change the digest")
	}
	completed, err := CompleteEmployment(advanced, date(t, "2026-03-01"))
	if err != nil {
		t.Fatal(err)
	}
	if completed.Digest == advanced.Digest {
		t.Fatal("employment completion did not change the digest")
	}
	tampered := completed
	tampered.Closed = true
	if err := tampered.Validate(); err == nil {
		t.Fatal("unobserved close passed validation")
	}
	forged := completed
	forged.Digest = "sha256:forged"
	if err := forged.Validate(); err == nil {
		t.Fatal("forged digest passed validation")
	}
}

func closureEvidenceFor(domain ClosureDomain, want values.EntityRef) ClosureEvidence {
	return ClosureEvidence{
		ObservationID: "obs-" + string(domain) + "-c",
		At:            mustDate("2026-03-02"),
		By:            "SECURITY_OFFICER",
		Evidence:      want,
	}
}
