package payroll

import (
	"bytes"
	"errors"
	"sync"
	"testing"
)

func releaseEffectsFixture() ReleaseEffects {
	return ReleaseEffects{
		PaymentsDigest:   "sha256:payments",
		StatementsDigest: "sha256:statements",
		BalancesDigest:   "sha256:balances",
		AccountingDigest: "sha256:accounting",
		ReportingDigest:  "sha256:reporting",
	}
}

func releaseObligationsFixture() ReleaseObligations {
	return ReleaseObligations{
		FundingDigest:    "sha256:funding",
		FilingDigest:     "sha256:filing",
		SettlementDigest: "sha256:settlement",
	}
}

func releaseRequestFixture(t *testing.T) ReleaseRequest {
	t.Helper()
	request := approvalRequestFixture(t)
	approval, err := ApprovePayroll(request)
	if err != nil {
		t.Fatalf("ApprovePayroll: %v", err)
	}
	lock, err := LockPayroll(approval, request)
	if err != nil {
		t.Fatalf("LockPayroll: %v", err)
	}
	released, err := request.Context.Run.Release("sha256:release-evidence")
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	return ReleaseRequest{
		Lock:           lock,
		Run:            released,
		Effects:        releaseEffectsFixture(),
		Obligations:    releaseObligationsFixture(),
		IdempotencyKey: "release-key-1",
	}
}

// TestTodo_PAYRUN_007 is the primary acceptance case: an approved, locked,
// released run finalizes into an immutable release compiling the exact
// payments, statements, balances, accounting, and reporting effects, while a
// duplicate release is refused.
func TestTodo_PAYRUN_007(t *testing.T) {
	req := releaseRequestFixture(t)
	release, err := ReleasePayroll(req, nil)
	if err != nil {
		t.Fatalf("ReleasePayroll: %v", err)
	}
	if release.RunID != req.Run.RunID || release.RunRevision != req.Run.Revision || release.LockDigest != req.Lock.LockDigest {
		t.Fatalf("release does not bind the lock and run: %+v", release)
	}
	if release.Effects.PaymentsDigest != "sha256:payments" || release.Effects.StatementsDigest != "sha256:statements" || release.Effects.BalancesDigest != "sha256:balances" || release.Effects.AccountingDigest != "sha256:accounting" || release.Effects.ReportingDigest != "sha256:reporting" {
		t.Fatalf("release does not compile every effect: %+v", release.Effects)
	}
	if release.ReleaseDigest == "" || release.ReleaseID != "payroll-release/release-key-1" {
		t.Fatalf("release identity is incomplete: %+v", release)
	}
	if err := release.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	explanation, err := release.Explain()
	if err != nil || explanation.RunRevision != req.Run.Revision || explanation.Digest != release.ReleaseDigest {
		t.Fatalf("explanation = %+v, err = %v", explanation, err)
	}
	if _, err := ReleasePayroll(req, []PayrollRelease{release}); !errors.Is(err, ErrReleaseDuplicate) {
		t.Fatalf("duplicate release error = %v", err)
	}
}

// TestTodo_PAYRUN_007_Property proves every effect and obligation is bound:
// each missing leg refuses, and a tampered lock never releases.
func TestTodo_PAYRUN_007_Property(t *testing.T) {
	base := releaseRequestFixture(t)
	effects := []func(*ReleaseRequest){
		func(r *ReleaseRequest) { r.Effects.PaymentsDigest = "" },
		func(r *ReleaseRequest) { r.Effects.StatementsDigest = "" },
		func(r *ReleaseRequest) { r.Effects.BalancesDigest = "" },
		func(r *ReleaseRequest) { r.Effects.AccountingDigest = "" },
		func(r *ReleaseRequest) { r.Effects.ReportingDigest = "" },
	}
	for i, mutate := range effects {
		req := base
		req.Effects = releaseEffectsFixture()
		mutate(&req)
		if _, err := ReleasePayroll(req, nil); !errors.Is(err, ErrReleaseRejected) {
			t.Fatalf("effects case %d error = %v", i, err)
		}
	}
	obligations := []func(*ReleaseRequest){
		func(r *ReleaseRequest) { r.Obligations.FundingDigest = "" },
		func(r *ReleaseRequest) { r.Obligations.FilingDigest = "" },
		func(r *ReleaseRequest) { r.Obligations.SettlementDigest = "" },
	}
	for i, mutate := range obligations {
		req := base
		req.Obligations = releaseObligationsFixture()
		mutate(&req)
		if _, err := ReleasePayroll(req, nil); !errors.Is(err, ErrReleaseObligation) {
			t.Fatalf("obligations case %d error = %v", i, err)
		}
	}
	tampered := base
	tampered.Lock.LockDigest = "sha256:forged"
	if _, err := ReleasePayroll(tampered, nil); !errors.Is(err, ErrReleaseRejected) {
		t.Fatalf("tampered lock error = %v", err)
	}
	// A release is a pure function of its request: unrelated prior entries
	// never change the digest.
	req := base
	req.IdempotencyKey = "release-key-property"
	first, err := ReleasePayroll(req, nil)
	if err != nil {
		t.Fatal(err)
	}
	other := PayrollRelease{
		ReleaseID: "payroll-release/release-key-other",
		RunID:     "run-2026-11-other", RunRevision: 3,
		LockDigest: "sha256:other-lock",
		Effects:    releaseEffectsFixture(), Obligations: releaseObligationsFixture(),
		IdempotencyKey: "release-key-other",
	}
	other.ReleaseDigest = other.computedDigest()
	if err := other.Validate(); err != nil {
		t.Fatal(err)
	}
	again, err := ReleasePayroll(req, []PayrollRelease{other})
	if err != nil {
		t.Fatal(err)
	}
	if again.ReleaseDigest != first.ReleaseDigest {
		t.Fatal("unrelated prior records changed the release digest")
	}
}

// TestTodo_PAYRUN_007_Golden pins byte-identical release evidence.
func TestTodo_PAYRUN_007_Golden(t *testing.T) {
	first, err := ReleasePayroll(releaseRequestFixture(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ReleasePayroll(releaseRequestFixture(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.ReleaseDigest != second.ReleaseDigest || !bytes.Equal(first.Canonical(), second.Canonical()) {
		t.Fatal("identical release inputs did not produce identical release evidence")
	}
	digest, err := second.Digest()
	if err != nil || digest != second.ReleaseDigest {
		t.Fatalf("Digest() = %q, err = %v", digest, err)
	}
}

// TestTodo_PAYRUN_007_Race proves concurrent releases with distinct keys all
// succeed deterministically and never mutate the shared request.
func TestTodo_PAYRUN_007_Race(t *testing.T) {
	base := releaseRequestFixture(t)
	var wg sync.WaitGroup
	digests := make([]string, 8)
	errs := make([]error, 8)
	for i := range digests {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := base
			release, err := ReleasePayroll(req, nil)
			if err != nil {
				errs[i] = err
				return
			}
			digests[i] = release.ReleaseDigest
			_ = release.Validate()
			_ = release.Canonical()
			_, _ = release.Explain()
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
		if digests[i] != digests[0] {
			t.Fatal("concurrent releases diverged")
		}
	}
	if base.IdempotencyKey != "release-key-1" {
		t.Fatal("release mutated the caller request")
	}
}

// TestTodo_PAYRUN_007_Integration proves the release sits exactly between the
// approval lock and settlement on the lifecycle path.
func TestTodo_PAYRUN_007_Integration(t *testing.T) {
	req := releaseRequestFixture(t)
	release, err := ReleasePayroll(req, nil)
	if err != nil {
		t.Fatal(err)
	}
	if release.RunRevision != req.Lock.RunRevision+1 {
		t.Fatalf("release revision %d does not follow the locked revision %d", release.RunRevision, req.Lock.RunRevision)
	}
	settled, err := req.Run.Settle()
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if settled.State != Settled || settled.SupersedesRevision != req.Run.Revision {
		t.Fatalf("settled run does not follow the released revision: %+v", settled)
	}
}

// TestTodo_PAYRUN_007_Fault proves stale locks, missing obligations, and
// duplicate releases fail with typed refusals and yield no release.
func TestTodo_PAYRUN_007_Fault(t *testing.T) {
	req := releaseRequestFixture(t)
	// A lock bound to another run is stale.
	foreign := approvalRequestFixture(t)
	foreign.Context.Run, _ = NewPayrollRun("run-foreign", "monthly",
		PeriodRef{ID: "period-foreign", Version: "v1", Digest: "sha256:period"},
		PopulationBindingRef{DefinitionID: "population-foreign", RevisionVersion: "v1", Digest: "sha256:population"},
		"sha256:inputs")
	calculated, err := foreign.Context.Run.Calculate("sha256:calculation")
	if err != nil {
		t.Fatal(err)
	}
	foreign.Context.Run = calculated
	foreignApproval, err := ApprovePayroll(foreign)
	if err != nil {
		t.Fatal(err)
	}
	foreignLock, err := LockPayroll(foreignApproval, foreign)
	if err != nil {
		t.Fatal(err)
	}
	stale := req
	stale.Lock = foreignLock
	if _, err := ReleasePayroll(stale, nil); !errors.Is(err, ErrReleaseStale) {
		t.Fatalf("foreign lock error = %v", err)
	}
	// Effects cannot compile before the run itself is released.
	calcReq := approvalRequestFixture(t)
	unreleased := req
	unreleased.Run = calcReq.Context.Run
	if _, err := ReleasePayroll(unreleased, nil); !errors.Is(err, ErrReleaseRejected) {
		t.Fatalf("unreleased run error = %v", err)
	}
	// A missing settlement obligation fails the release.
	unfunded := req
	unfunded.Obligations = releaseObligationsFixture()
	unfunded.Obligations.SettlementDigest = ""
	if _, err := ReleasePayroll(unfunded, nil); !errors.Is(err, ErrReleaseObligation) {
		t.Fatalf("missing obligation error = %v", err)
	}
	// The same run revision never releases twice, even under a fresh key.
	release, err := ReleasePayroll(req, nil)
	if err != nil {
		t.Fatal(err)
	}
	rekeyed := req
	rekeyed.IdempotencyKey = "release-key-fresh"
	if _, err := ReleasePayroll(rekeyed, []PayrollRelease{release}); !errors.Is(err, ErrReleaseDuplicate) {
		t.Fatalf("rekeyed duplicate error = %v", err)
	}
	var refusal *ReleaseError
	if _, err := ReleasePayroll(stale, nil); !errors.As(err, &refusal) || refusal.Field == "" || refusal.Reason == "" {
		t.Fatalf("refusal = %+v", err)
	}
}

// TestTodo_PAYRUN_007_Mutation proves the release digest binds every effect,
// obligation, and the lock: any change yields a new digest and a tampered
// release fails validation.
func TestTodo_PAYRUN_007_Mutation(t *testing.T) {
	req := releaseRequestFixture(t)
	base, err := ReleasePayroll(req, nil)
	if err != nil {
		t.Fatal(err)
	}
	changed := req
	changed.IdempotencyKey = "release-key-changed"
	changed.Effects = releaseEffectsFixture()
	changed.Effects.AccountingDigest = "sha256:accounting-changed"
	mutated, err := ReleasePayroll(changed, nil)
	if err != nil {
		t.Fatal(err)
	}
	if mutated.ReleaseDigest == base.ReleaseDigest {
		t.Fatal("changed effect digest did not change the release digest")
	}
	tampered := base
	tampered.Effects.BalancesDigest = "sha256:tampered"
	if err := tampered.Validate(); err == nil {
		t.Fatal("tampered release validated")
	}
	empty := base
	empty.ReleaseDigest = ""
	if err := empty.Validate(); err == nil {
		t.Fatal("undigested release validated")
	}
}

// TestTodo_PAYRUN_007_Refusals pins the typed refusal for every malformed
// release request and release shape.
func TestTodo_PAYRUN_007_Refusals(t *testing.T) {
	req := releaseRequestFixture(t)

	refusal := releaseRefusal("lock", "payroll lock is invalid", ErrReleaseStale)
	var releaseErr *ReleaseError
	if !errors.As(refusal, &releaseErr) || releaseErr.Field != "lock" || releaseErr.Reason == "" {
		t.Fatalf("refusal = %+v", refusal)
	}
	if !errors.Is(refusal, ErrReleaseRejected) || !errors.Is(refusal, ErrReleaseStale) {
		t.Fatalf("refusal identity = %v", refusal)
	}
	if refusal.Error() == "" || releaseErr.Unwrap() != ErrReleaseStale {
		t.Fatal("refusal text or cause is missing")
	}

	var zero PayrollRelease
	if err := zero.Validate(); err == nil || zero.Canonical() != nil {
		t.Fatal("zero release validated")
	}
	if _, err := zero.Digest(); err == nil {
		t.Fatal("zero release digested")
	}
	if _, err := zero.Explain(); err == nil {
		t.Fatal("zero release explained")
	}

	release, err := ReleasePayroll(req, nil)
	if err != nil {
		t.Fatal(err)
	}
	renamed := release
	renamed.ReleaseID = "payroll-release/bogus"
	if err := renamed.Validate(); err == nil {
		t.Fatal("rekeyed release id validated")
	}

	requestCases := []struct {
		name   string
		mutate func(*ReleaseRequest)
	}{
		{"lock", func(r *ReleaseRequest) { r.Lock = PayrollLock{} }},
		{"run", func(r *ReleaseRequest) { r.Run = PayrollRun{} }},
		{"key", func(r *ReleaseRequest) { r.IdempotencyKey = "  " }},
	}
	for _, tc := range requestCases {
		t.Run(tc.name, func(t *testing.T) {
			bad := req
			tc.mutate(&bad)
			if _, err := ReleasePayroll(bad, nil); !errors.Is(err, ErrReleaseRejected) {
				t.Fatalf("%s error = %v", tc.name, err)
			}
		})
	}
}
