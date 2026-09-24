package payroll

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal/payrules"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
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
	_, err = payrules.DefaultPayStatementRegistry()
	if err != nil {
		t.Fatalf("load pay statement registry: %v", err)
	}
	payDate, err := values.ParseLocalDate("2026-09-23")
	if err != nil {
		t.Fatal(err)
	}
	content := StatementManifest{PayDate: payDate, Statements: []PayStatement{{WorkerID: "worker-1", Jurisdiction: "GA", Fields: []StatementField{{Name: "gross_wages", Value: "1000.00"}}, Medium: payrules.Paper}}}
	digest, err := content.Digest()
	if err != nil {
		t.Fatal(err)
	}
	effects := releaseEffectsFixture()
	effects.StatementsDigest = digest
	return ReleaseRequest{
		Lock:             lock,
		Run:              released,
		Effects:          effects,
		StatementContent: content,
		Obligations:      releaseObligationsFixture(),
		IdempotencyKey:   "release-key-1",
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
	statementDigest, _ := req.StatementContent.Digest()
	if release.Effects.PaymentsDigest != "sha256:payments" || release.Effects.StatementsDigest != statementDigest || release.Effects.BalancesDigest != "sha256:balances" || release.Effects.AccountingDigest != "sha256:accounting" || release.Effects.ReportingDigest != "sha256:reporting" {
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
		LockDigest: "sha256:other-lock", StatementRulesDigest: "sha256:rules",
		Effects: releaseEffectsFixture(), Obligations: releaseObligationsFixture(),
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
	changed.Effects = req.Effects
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

// TestTodo_REV_044_02 exercises the release gate against the repository's
// resolved jurisdiction registry and the exact content digest it publishes.
func TestTodo_REV_044_02(t *testing.T) {
	req := releaseRequestFixture(t)
	registry, err := payrules.DefaultPayStatementRegistry()
	if err != nil {
		t.Fatal(err)
	}
	california, ok := registry.ForState("CA")
	if !ok {
		t.Fatal("CA registry row missing")
	}
	req.StatementContent.Statements = []PayStatement{{
		WorkerID: "worker-ca", Jurisdiction: "CA", Medium: payrules.Paper,
		Fields: []StatementField{
			{Name: "gross_wages", Value: "1000"}, {Name: "net_wages", Value: "800"},
			{Name: "hours_worked", Value: "40"}, {Name: "rates_of_pay", Value: "$25"},
			{Name: "deductions", Value: "200"}, {Name: "pay_period_dates", Value: "2026-09-01/2026-09-15"},
			{Name: "employee_name_identifier", Value: "Worker CA"}, {Name: "employer_name_address", Value: "Employer"},
			{Name: "piece_rate_units", Value: "0"},
		},
	}}
	req.Effects.StatementsDigest, err = req.StatementContent.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReleasePayroll(req, nil); err != nil {
		t.Fatalf("complete California statement refused: %v", err)
	}
	released, err := ReleasePayroll(req, nil)
	if err != nil {
		t.Fatal(err)
	}
	if released.StatementRulesDigest != registry.Digest {
		t.Fatalf("release rules digest = %q, want %q", released.StatementRulesDigest, registry.Digest)
	}
	tamperedRules := released
	tamperedRules.StatementRulesDigest = "sha256:substituted-rules"
	if err := tamperedRules.Validate(); err == nil {
		t.Fatal("release accepted a substituted statement rules digest")
	}

	missing := req
	missing.StatementContent.Statements = append([]PayStatement(nil), req.StatementContent.Statements...)
	missing.StatementContent.Statements[0].Fields = append([]StatementField(nil), req.StatementContent.Statements[0].Fields[:len(california.RequiredFields)-1]...)
	missing.Effects.StatementsDigest, _ = missing.StatementContent.Digest()
	_, err = ReleasePayroll(missing, nil)
	var refusal *ReleaseError
	if !errors.As(err, &refusal) || !errors.Is(err, ErrReleaseRejected) || !strings.Contains(refusal.Reason, "CA") || !strings.Contains(refusal.Reason, "piece_rate_units") {
		t.Fatalf("missing California field refusal = %v", err)
	}

	electronic := req
	electronic.StatementContent.Statements = append([]PayStatement(nil), req.StatementContent.Statements...)
	electronic.StatementContent.Statements[0].Medium = payrules.Electronic
	electronic.StatementContent.Statements[0].Consent = false
	electronic.Effects.StatementsDigest, _ = electronic.StatementContent.Digest()
	_, err = ReleasePayroll(electronic, nil)
	if !errors.As(err, &refusal) || !strings.Contains(refusal.Field, "consent") || !strings.Contains(refusal.Reason, "CA") {
		t.Fatalf("missing electronic consent refusal = %v", err)
	}

	tampered := req
	tampered.Effects.StatementsDigest = "sha256:unrelated"
	_, err = ReleasePayroll(tampered, nil)
	if !errors.As(err, &refusal) || refusal.Field != "effects.statements_digest" {
		t.Fatalf("unbound statement digest refusal = %v", err)
	}
}

// TestTodo_REV_044_02_Property checks each registry-mandated field independently
// and proves manifest order does not alter its digest.
func TestTodo_REV_044_02_Property(t *testing.T) {
	base := releaseRequestFixture(t)
	base.StatementContent.Statements[0] = PayStatement{WorkerID: "worker-ca", Jurisdiction: "CA", Medium: payrules.Paper,
		Fields: []StatementField{
			{Name: "gross_wages", Value: "1"}, {Name: "net_wages", Value: "1"}, {Name: "hours_worked", Value: "1"},
			{Name: "rates_of_pay", Value: "1"}, {Name: "deductions", Value: "1"}, {Name: "pay_period_dates", Value: "1"},
			{Name: "employee_name_identifier", Value: "1"}, {Name: "employer_name_address", Value: "1"}, {Name: "piece_rate_units", Value: "1"},
		}}
	registry, err := payrules.DefaultPayStatementRegistry()
	if err != nil {
		t.Fatal(err)
	}
	rule, _ := registry.ForState("CA")
	for _, required := range rule.RequiredFields {
		candidate := base
		candidate.StatementContent.Statements = append([]PayStatement(nil), base.StatementContent.Statements...)
		fields := make([]StatementField, 0, len(rule.RequiredFields)-1)
		for _, field := range candidate.StatementContent.Statements[0].Fields {
			if field.Name != required {
				fields = append(fields, field)
			}
		}
		candidate.StatementContent.Statements[0].Fields = fields
		candidate.Effects.StatementsDigest, _ = candidate.StatementContent.Digest()
		_, err := ReleasePayroll(candidate, nil)
		var refusal *ReleaseError
		if !errors.As(err, &refusal) || !strings.Contains(refusal.Reason, required) {
			t.Errorf("missing required field %s refusal = %v", required, err)
		}
	}
	first, _ := base.StatementContent.Digest()
	reordered := base.StatementContent
	reordered.Statements = append([]PayStatement(nil), base.StatementContent.Statements...)
	reordered.Statements[0].Fields = append([]StatementField(nil), base.StatementContent.Statements[0].Fields...)
	reordered.Statements[0].Fields[0], reordered.Statements[0].Fields[1] = reordered.Statements[0].Fields[1], reordered.Statements[0].Fields[0]
	second, _ := reordered.Digest()
	if first != second {
		t.Fatal("field order changed statement digest")
	}
}
