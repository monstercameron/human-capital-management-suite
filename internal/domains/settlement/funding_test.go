package settlement

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/payroll"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func fundingDecimal(t *testing.T, text string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func fundingDate(t *testing.T) values.LocalDate {
	t.Helper()
	d, err := values.NewLocalDate(2026, time.November, 30)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func fundingEarlier(t *testing.T) values.LocalDate {
	t.Helper()
	d, err := values.NewLocalDate(2026, time.November, 28)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func fundingRun(t *testing.T, id string) payroll.PayrollRun {
	t.Helper()
	run, err := payroll.NewPayrollRun(id, "monthly",
		payroll.PeriodRef{ID: "period", Version: "v1", Digest: "sha256:period"},
		payroll.PopulationBindingRef{DefinitionID: "population", RevisionVersion: "v1", Digest: "sha256:population"}, "sha256:inputs")
	if err != nil {
		t.Fatal(err)
	}
	run, err = run.Calculate("sha256:calculation")
	if err != nil {
		t.Fatal(err)
	}
	run, err = run.Release("sha256:release")
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func fundingInstruction(t *testing.T, run payroll.PayrollRun, id, payee, amount string) PaymentInstruction {
	t.Helper()
	spec := PaymentInstructionSpec{
		InstructionID: id, PayeeRef: payee, Amount: fundingDecimal(t, amount),
		Currency: "USD", FundingSourceRef: "funding:operating", Rail: RailACH,
		BankDetailRef: "bank-detail:token-1", ScheduleRef: "schedule:2026-11-30",
		ValueDate: fundingDate(t),
	}
	i, err := NewPaymentInstruction(run, spec)
	if err != nil {
		t.Fatal(err)
	}
	return i
}

func fundingRequestFixture(t *testing.T) FundingRequest {
	t.Helper()
	run := fundingRun(t, "run-funding")
	return FundingRequest{
		Entity:    "entity-1",
		Currency:  "USD",
		ValueDate: fundingDate(t),
		Instructions: []PaymentInstruction{
			fundingInstruction(t, run, "instruction-1", "worker:alice", "100.00"),
			fundingInstruction(t, run, "instruction-2", "worker:bob", "50.00"),
		},
		WorkerTotals: []WorkerFundingTotal{
			{WorkerRef: "worker:alice", Amount: fundingDecimal(t, "100.00")},
			{WorkerRef: "worker:bob", Amount: fundingDecimal(t, "50.00")},
		},
		ControlTotal: fundingDecimal(t, "150.00"),
		Source: FundingSource{
			Ref: "funding:operating", Entity: "entity-1", Currency: "USD",
			AvailableAmount: fundingDecimal(t, "200.00"), AvailableBy: fundingEarlier(t),
		},
	}
}

// TestTodo_SETTLE_002 is the PRIMARY contract: worker, instruction and
// control totals reconcile exactly by entity, currency and date, and a
// missing or late funding source blocks release with an explicit obligation.
func TestTodo_SETTLE_002(t *testing.T) {
	req := fundingRequestFixture(t)
	got, obligation, err := BuildFundingRequirement(req)
	if err != nil {
		t.Fatalf("BuildFundingRequirement: %v", err)
	}
	if obligation.Required() {
		t.Fatalf("funded requirement carries an obligation: %+v", obligation)
	}
	expect := fundingDecimal(t, "150.00")
	for name, total := range map[string]values.Decimal{
		"worker": got.WorkerTotal, "instruction": got.InstructionTotal, "control": got.ControlTotal,
	} {
		if total.Cmp(expect) != 0 {
			t.Fatalf("%s total = %s, want 150.00", name, total.String())
		}
	}
	if got.Entity != "entity-1" || got.Currency != "USD" || got.SourceRef != "funding:operating" {
		t.Fatalf("requirement scope = %+v", got)
	}
	if got.InstructionCount != 2 || got.InstructionDigest == "" || got.CanonicalDigest == "" {
		t.Fatalf("requirement identity is incomplete: %+v", got)
	}
	if len(got.RuleTrace) == 0 {
		t.Fatal("funded requirement must carry a rule trace")
	}
	again, _, err := BuildFundingRequirement(req)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if again.CanonicalDigest != got.CanonicalDigest {
		t.Fatal("funding requirement is not deterministic")
	}

	mutate := func(change func(*FundingRequest)) FundingRequest {
		next := fundingRequestFixture(t)
		change(&next)
		return next
	}
	run := fundingRun(t, "run-funding")
	cases := map[string]struct {
		req       FundingRequest
		obligated bool
		kind      string
	}{
		"worker totals mismatch instruction total": {
			req: mutate(func(r *FundingRequest) {
				r.WorkerTotals[1].Amount = fundingDecimal(t, "40.00")
			}),
		},
		"control total mismatch": {
			req: mutate(func(r *FundingRequest) {
				r.ControlTotal = fundingDecimal(t, "149.99")
			}),
		},
		"instruction currency differs": {
			req: mutate(func(r *FundingRequest) {
				bad := fundingInstruction(t, run, "instruction-3", "worker:zed", "10.00")
				bad.Currency = "EUR"
				bad.CanonicalDigest = bad.computedDigest()
				r.Instructions = append(r.Instructions, bad)
			}),
		},
		"instruction value date differs": {
			req: mutate(func(r *FundingRequest) {
				late, err := values.NewLocalDate(2026, time.December, 1)
				if err != nil {
					t.Fatal(err)
				}
				bad := fundingInstruction(t, run, "instruction-3", "worker:zed", "10.00")
				bad.ValueDate = late
				bad.CanonicalDigest = bad.computedDigest()
				r.Instructions = append(r.Instructions, bad)
			}),
		},
		"missing funding source blocks with obligation": {
			req: mutate(func(r *FundingRequest) {
				r.Source.Ref = ""
			}),
			obligated: true,
			kind:      FundingMissing,
		},
		"insufficient funding blocks with obligation": {
			req: mutate(func(r *FundingRequest) {
				r.Source.AvailableAmount = fundingDecimal(t, "100.00")
			}),
			obligated: true,
			kind:      FundingInsufficient,
		},
		"late funding blocks with obligation": {
			req: mutate(func(r *FundingRequest) {
				late, err := values.NewLocalDate(2026, time.December, 1)
				if err != nil {
					t.Fatal(err)
				}
				r.Source.AvailableBy = late
			}),
			obligated: true,
			kind:      FundingLate,
		},
		"empty instruction set is never funded": {
			req: mutate(func(r *FundingRequest) {
				r.Instructions = nil
				r.WorkerTotals = nil
				r.ControlTotal = fundingDecimal(t, "0.00")
			}),
		},
		"duplicate instruction identity is never funded twice": {
			req: mutate(func(r *FundingRequest) {
				r.Instructions = append(r.Instructions, r.Instructions[0])
			}),
		},
	}
	for name, tc := range cases {
		_, obligation, err := BuildFundingRequirement(tc.req)
		if !errors.Is(err, ErrFundingRejected) {
			t.Fatalf("%s: err = %v, want SETTLE_002_REJECTED", name, err)
		}
		if tc.obligated {
			if !obligation.Required() || obligation.Kind != tc.kind {
				t.Fatalf("%s: obligation = %+v, want kind %s", name, obligation, tc.kind)
			}
		}
	}
}

// TestTodo_SETTLE_002_Race proves concurrent funding builds observe one
// deterministic requirement with no shared mutable state.
func TestTodo_SETTLE_002_Race(t *testing.T) {
	req := fundingRequestFixture(t)
	const callers = 16
	digests := make([]string, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for n := range callers {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			got, _, err := BuildFundingRequirement(req)
			if err != nil {
				errs[n] = err
				return
			}
			digests[n] = got.CanonicalDigest
		}(n)
	}
	wg.Wait()
	for n := range callers {
		if errs[n] != nil {
			t.Fatalf("caller %d: %v", n, errs[n])
		}
		if digests[n] != digests[0] || digests[n] == "" {
			t.Fatalf("caller %d digest = %q, want %q", n, digests[n], digests[0])
		}
	}
}

// TestTodo_SETTLE_002_Integration proves the funding requirement binds the
// exact released payroll revision through real cross-package instructions:
// only instructions pointing at the released run digest fund, and a different
// run produces a different requirement digest.
func TestTodo_SETTLE_002_Integration(t *testing.T) {
	run := fundingRun(t, "run-integration")
	req := FundingRequest{
		Entity:    "entity-1",
		Currency:  "USD",
		ValueDate: fundingDate(t),
		Instructions: []PaymentInstruction{
			fundingInstruction(t, run, "instruction-1", "worker:alice", "100.00"),
		},
		WorkerTotals: []WorkerFundingTotal{
			{WorkerRef: "worker:alice", Amount: fundingDecimal(t, "100.00")},
		},
		ControlTotal: fundingDecimal(t, "100.00"),
		Source: FundingSource{
			Ref: "funding:operating", Entity: "entity-1", Currency: "USD",
			AvailableAmount: fundingDecimal(t, "100.00"), AvailableBy: fundingEarlier(t),
		},
	}
	got, _, err := BuildFundingRequirement(req)
	if err != nil {
		t.Fatalf("BuildFundingRequirement: %v", err)
	}
	for _, i := range req.Instructions {
		if i.PayrollRunRef != run.CanonicalDigest {
			t.Fatalf("instruction run ref = %s, want %s", i.PayrollRunRef, run.CanonicalDigest)
		}
	}
	other := fundingRun(t, "run-other")
	otherReq := req
	otherReq.Instructions = []PaymentInstruction{
		fundingInstruction(t, other, "instruction-1", "worker:alice", "100.00"),
	}
	otherGot, _, err := BuildFundingRequirement(otherReq)
	if err != nil {
		t.Fatalf("rebuild on other run: %v", err)
	}
	if otherGot.CanonicalDigest == got.CanonicalDigest {
		t.Fatal("funding digest must bind the exact released run")
	}
}

// TestTodo_SETTLE_002_Fault proves failure handling stays reconcilable: a
// repeated build funds nothing twice, a conflicting natural key is rejected
// rather than merged, and a tampered instruction fails closed.
func TestTodo_SETTLE_002_Fault(t *testing.T) {
	req := fundingRequestFixture(t)
	first, _, err := BuildFundingRequirement(req)
	if err != nil {
		t.Fatalf("first build: %v", err)
	}
	second, _, err := BuildFundingRequirement(req)
	if err != nil {
		t.Fatalf("second build: %v", err)
	}
	if first.CanonicalDigest != second.CanonicalDigest {
		t.Fatal("repeated funding build must be identical, never a second funding")
	}

	run := fundingRun(t, "run-funding")
	conflict := fundingRequestFixture(t)
	twin := fundingInstruction(t, run, "instruction-9", "worker:alice", "100.00")
	conflict.Instructions = append(conflict.Instructions, twin)
	if _, _, err := BuildFundingRequirement(conflict); !errors.Is(err, ErrFundingRejected) {
		t.Fatalf("natural-key conflict: err = %v, want SETTLE_002_REJECTED", err)
	}

	tampered := fundingRequestFixture(t)
	tampered.Instructions[0].Amount = fundingDecimal(t, "999.00")
	if _, _, err := BuildFundingRequirement(tampered); !errors.Is(err, ErrFundingRejected) {
		t.Fatalf("tampered instruction: err = %v, want SETTLE_002_REJECTED", err)
	}
}

// TestTodo_SETTLE_002_Mutation kills the seeded defect class for this todo:
// provider acceptance is never funding, returns never fund, totals never pass
// while components are omitted, and the funding check is never skippable.
func TestTodo_SETTLE_002_Mutation(t *testing.T) {
	run := fundingRun(t, "run-funding")
	advance := func(i PaymentInstruction, to SettlementState, evidence string) PaymentInstruction {
		t.Helper()
		next, err := i.Transition(to, evidence)
		if err != nil {
			t.Fatal(err)
		}
		return next
	}
	accepted := advance(fundingInstruction(t, run, "instruction-1", "worker:alice", "100.00"), StateSubmitted, "provider:submitted")
	accepted = advance(accepted, StateAcknowledged, "provider:acknowledged")
	settled := advance(accepted, StateSettled, "rail:settled")
	returned := advance(settled, StateReturned, "rail:returned")

	base := fundingRequestFixture(t)
	for name, instruction := range map[string]PaymentInstruction{
		"acceptance is not funding": accepted,
		"settlement is not funding": settled,
		"returns never fund":        returned,
	} {
		req := base
		req.Instructions = []PaymentInstruction{instruction}
		if _, _, err := BuildFundingRequirement(req); !errors.Is(err, ErrFundingRejected) {
			t.Fatalf("%s: err = %v, want SETTLE_002_REJECTED", name, err)
		}
	}

	omitted := fundingRequestFixture(t)
	omitted.WorkerTotals = []WorkerFundingTotal{
		{WorkerRef: "worker:alice", Amount: fundingDecimal(t, "100.00")},
	}
	omitted.ControlTotal = fundingDecimal(t, "150.00")
	if _, _, err := BuildFundingRequirement(omitted); !errors.Is(err, ErrFundingRejected) {
		t.Fatalf("omitted worker line: err = %v, want SETTLE_002_REJECTED", err)
	}

	unfunded := fundingRequestFixture(t)
	unfunded.Source.AvailableAmount = fundingDecimal(t, "0.00")
	if _, obligation, err := BuildFundingRequirement(unfunded); !errors.Is(err, ErrFundingRejected) {
		t.Fatalf("skipped funding check: err = %v, want SETTLE_002_REJECTED", err)
	} else if !obligation.Required() || obligation.Kind != FundingInsufficient {
		t.Fatalf("skipped funding check: obligation = %+v", obligation)
	}
}
