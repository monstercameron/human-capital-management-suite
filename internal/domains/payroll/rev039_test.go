package payroll

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/balance"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func rev039Definition() balance.AccumulatorDefinition {
	return balance.AccumulatorDefinition{
		ID: "pto", Version: "1.0.0", Name: "Paid time off",
		Unit: "hour", Currency: "USD", Subject: "worker", Period: balance.PeriodCalendarYear,
		Dimensions: []balance.BalanceDimension{{Name: "worker_id", ValueType: "entity_id", Required: true}, {Name: "program", ValueType: "reference", Required: true}},
		EntryTypes: []string{"GRANT", "USAGE", "CORRECTION"},
		Authority:  balance.Authority{SourceID: "rewards.balance", Version: "2026.1"},
		Floor:      balance.PolicyRule{Kind: balance.RuleNone, Version: "1"}, Cap: balance.PolicyRule{Kind: balance.RuleConfigured, Version: "1", Value: "40"},
		Expiry: balance.PolicyRule{Kind: balance.RuleConfigured, Version: "1", Value: "end_of_year"}, Rollover: balance.PolicyRule{Kind: balance.RuleNone, Version: "1"}, Correction: balance.PolicyRule{Kind: balance.RuleConfigured, Version: "1", Value: "append_only"},
	}
}

func rev039Instant(t *testing.T, text string) values.Instant {
	t.Helper()
	v, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatal(err)
	}
	return values.NewInstant(v)
}

func rev039Entries(t *testing.T) (original, corrected balance.BalanceEntry) {
	t.Helper()
	at := rev039Instant(t, "2026-06-01T00:00:00Z")
	recorded := rev039Instant(t, "2026-06-02T00:00:00Z")
	original = balance.BalanceEntry{AccountID: "worker-1", DefinitionID: "pto", DefinitionVersion: "1.0.0", Unit: "hour", Currency: "USD", Subject: "worker", Period: string(balance.PeriodCalendarYear), Dimensions: map[string]string{"worker_id": "worker-1", "program": "pto"}, Kind: balance.Credit, Amount: values.MustDecimal("10.00", 2, values.RoundingExactRequired), EntryType: "GRANT", SourceTransactionID: "leave-start-1", IdempotencyKey: "leave-start-1"}
	original.EffectiveAt, original.RecordedAt, original.AuthorizedAt = at, recorded, recorded
	corrected = original
	corrected.Amount = values.MustDecimal("15.00", 2, values.RoundingExactRequired)
	corrected.SourceTransactionID = "leave-correction-1"
	corrected.IdempotencyKey = "leave-correction-1"
	corrected.RecordedAt, corrected.AuthorizedAt = recorded, recorded
	return original, corrected
}

func rev039Deps() []balance.CorrectionDependency {
	return []balance.CorrectionDependency{{ID: "balance-result", Owner: "balance", Version: "balance/1"}, {ID: "payroll-result", Owner: "payroll", Version: RecalculationVersion, DependsOn: []string{"balance-result"}}, {ID: "tax-result", Owner: "tax", Version: "tax/1", DependsOn: []string{"payroll-result"}}, {ID: "benefits-result", Owner: "benefits", Version: "benefits/1", DependsOn: []string{"tax-result"}}}
}

func rev039Impact(t *testing.T, result balance.RetroCorrectionResult, id string) balance.CorrectionImpact {
	t.Helper()
	for _, impact := range result.Impacts {
		if impact.ID == id {
			return impact
		}
	}
	t.Fatalf("no impact %q in %+v", id, result.Impacts)
	return balance.CorrectionImpact{}
}

// TestTodo_REV_039_02 is the REV-039-02 primary: the payroll domain
// registers a real BAL-006 dependent recalculator that re-runs the governed
// pay-run with the corrected balance ending as its year-to-date accumulator
// and returns version-pinned evidence bound to the corrected balance. The
// recomputed output is observable through the sink; dependents without an
// implementation honestly stay reconciliation-required.
func TestTodo_REV_039_02(t *testing.T) {
	d := rev039Definition()
	original, corrected := rev039Entries(t)
	var sink RecalculatedPayRun
	result, err := balance.ApplyRetroCorrection(balance.RetroCorrectionRequest{
		Definition: d, Original: original, Corrected: corrected, Ledger: []balance.BalanceEntry{original},
		Opening:              values.MustDecimal("0.00", 2, values.RoundingExactRequired),
		ExpectedDependencies: rev039Deps(), Dependencies: rev039Deps(),
		Recalculators: map[string]balance.DependentRecalculator{"payroll-result": RecalculatorForPayRun(payRunFixture(t), &sink)},
	})
	if err != nil {
		t.Fatalf("ApplyRetroCorrection: %v", err)
	}
	if result.Balance.Ending.String() != "15.00" {
		t.Fatalf("corrected ending = %s, want 15.00", result.Balance.Ending)
	}
	payroll := rev039Impact(t, result, "payroll-result")
	if payroll.State != balance.RecalculationComplete || payroll.EvidenceDigest == "" {
		t.Fatalf("payroll impact = %+v, want RECALCULATED with evidence", payroll)
	}
	for _, id := range []string{"tax-result", "benefits-result"} {
		impact := rev039Impact(t, result, id)
		if impact.State != balance.ReconciliationRequired || impact.EvidenceDigest != "" {
			t.Fatalf("impact %s = %+v, want RECONCILIATION_REQUIRED without evidence", id, impact)
		}
	}
	if sink.Input.PriorAccumulator.String() != "15.00" {
		t.Fatalf("recomputed accumulator = %s, want the corrected 15.00 ending", sink.Input.PriorAccumulator)
	}
	if sink.Result.Wage.GrossPay.String() != "1100.00" {
		t.Fatalf("recomputed gross = %s, want 1100.00 (wage leg is accumulator-independent)", sink.Result.Wage.GrossPay)
	}
	accrued, err := sink.Result.Tax.NewAccumulator.Sub(sink.Input.PriorAccumulator)
	if err != nil {
		t.Fatal(err)
	}
	period, err := sink.Result.Tax.EmployeeTax.Add(sink.Result.Tax.EmployerTax)
	if err != nil {
		t.Fatal(err)
	}
	if accrued.String() != period.String() {
		t.Fatalf("accrued %s != period tax %s: accumulator math is wrong", accrued, period)
	}
	if sink.Result.Tax.RuleVersion != "2026-v1" {
		t.Fatalf("tax rule version = %q, want the pinned pack version", sink.Result.Tax.RuleVersion)
	}
	if sink.Result.Package.Digest == "" || sink.Result.SourceValueDigest == "" {
		t.Fatal("recomputed pay run is missing its sealed filing package")
	}
}

// TestTodo_REV_039_02_Integration posts a real BAL-002 correction through
// the entry store and observes the payroll domain's recomputed output end
// to end: the corrected ending flows into the rerun accumulator, the
// payroll impact carries version-pinned evidence, and the whole correction
// seals deterministically.
func TestTodo_REV_039_02_Integration(t *testing.T) {
	d := rev039Definition()
	original, corrected := rev039Entries(t)
	clock := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
	store := balance.NewEntryStoreWithClock(func() time.Time { return clock })
	receipt, err := store.Post(balance.PostRequest{Entry: original, ExpectedHead: 0}, d)
	if err != nil {
		t.Fatalf("post original: %v", err)
	}
	if receipt.Head != 1 || receipt.Digest == "" {
		t.Fatalf("post receipt = %+v", receipt)
	}
	posted := receipt.Entry
	run := func() (balance.RetroCorrectionResult, RecalculatedPayRun) {
		var sink RecalculatedPayRun
		result, err := balance.ApplyRetroCorrection(balance.RetroCorrectionRequest{
			Definition: d, Original: posted, Corrected: corrected, Ledger: store.Entries("worker-1"),
			Opening:              values.MustDecimal("0.00", 2, values.RoundingExactRequired),
			ExpectedDependencies: rev039Deps(), Dependencies: rev039Deps(),
			Recalculators: map[string]balance.DependentRecalculator{"payroll-result": RecalculatorForPayRun(payRunFixture(t), &sink)},
		})
		if err != nil {
			t.Fatalf("ApplyRetroCorrection: %v", err)
		}
		return result, sink
	}
	first, sink := run()
	second, _ := run()
	if first.Digest != second.Digest {
		t.Fatal("identical posted corrections sealed different digests")
	}
	payroll := rev039Impact(t, first, "payroll-result")
	if payroll.State != balance.RecalculationComplete {
		t.Fatalf("payroll impact = %+v", payroll)
	}
	if sink.Input.PriorAccumulator.String() != "15.00" || sink.Result.Tax.NewAccumulator.String() == "0.00" {
		t.Fatalf("recomputed output does not carry the correction: %+v", sink.Result.Tax)
	}
	// The recomputed output differs from a pre-correction run: the
	// correction genuinely flows into payroll, it is not a constant.
	uncorrected, err := RunPayrollPeriod(payRunFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if sink.Result.Tax.NewAccumulator.String() == uncorrected.Tax.NewAccumulator.String() {
		t.Fatal("corrected accumulator equals the pre-correction accumulator")
	}
	if sink.Result.Package.Digest == uncorrected.Package.Digest {
		t.Fatal("corrected filing package equals the pre-correction package")
	}
}

// TestTodo_REV_039_02_Mutation kills the drift mutants: version drift,
// non-money endings, broken pay-run inputs and a nil sink all fail the
// correction closed instead of claiming a recomputation that is not.
func TestTodo_REV_039_02_Mutation(t *testing.T) {
	d := rev039Definition()
	original, corrected := rev039Entries(t)
	build := func(deps []balance.CorrectionDependency, base PayRunInput, sink *RecalculatedPayRun, amount string, scale int32) balance.RetroCorrectionRequest {
		fixed := corrected
		fixed.Amount = values.MustDecimal(amount, scale, values.RoundingExactRequired)
		return balance.RetroCorrectionRequest{
			Definition: d, Original: original, Corrected: fixed, Ledger: []balance.BalanceEntry{original},
			Opening:              values.MustDecimal("0.00", 2, values.RoundingExactRequired),
			ExpectedDependencies: deps, Dependencies: deps,
			Recalculators: map[string]balance.DependentRecalculator{"payroll-result": RecalculatorForPayRun(base, sink)},
		}
	}
	drifted := rev039Deps()
	drifted[1].Version = "payroll/2"
	if _, err := balance.ApplyRetroCorrection(build(drifted, payRunFixture(t), &RecalculatedPayRun{}, "15.00", 2)); !errors.Is(err, balance.ErrCorrectionInvalid) {
		t.Fatalf("version drift err = %v, want ErrCorrectionInvalid", err)
	}
	if _, err := balance.ApplyRetroCorrection(build(rev039Deps(), payRunFixture(t), &RecalculatedPayRun{}, "15.005", 3)); !errors.Is(err, balance.ErrCorrectionInvalid) {
		t.Fatalf("sub-cent correction err = %v, want ErrCorrectionInvalid", err)
	}
	// The real recalculator names the accumulator refusal when a
	// non-cent ending reaches it directly.
	subcent := balance.RecalculationContext{
		Dependent: balance.CorrectionDependency{ID: "payroll-result", Owner: "payroll", Version: RecalculationVersion},
		Balance:   balance.AuthorizedBalance{Ending: values.MustDecimal("15.005", 3, values.RoundingExactRequired), Digest: "sha256:test"},
	}
	if _, err := RecalculatorForPayRun(payRunFixture(t), &RecalculatedPayRun{})(subcent); !errors.Is(err, ErrRecalculationIncompatible) {
		t.Fatalf("sub-cent ending err = %v, want ErrRecalculationIncompatible", err)
	} else if !strings.Contains(err.Error(), "accumulator") {
		t.Fatalf("sub-cent ending err = %v, want the accumulator refusal named", err)
	}
	broken := payRunFixture(t)
	broken.Wage.Approved = false
	if _, err := balance.ApplyRetroCorrection(build(rev039Deps(), broken, &RecalculatedPayRun{}, "15.00", 2)); !errors.Is(err, balance.ErrCorrectionInvalid) {
		t.Fatalf("broken pay-run input err = %v, want ErrCorrectionInvalid", err)
	}
	if _, err := balance.ApplyRetroCorrection(build(rev039Deps(), payRunFixture(t), nil, "15.00", 2)); !errors.Is(err, balance.ErrCorrectionInvalid) {
		t.Fatalf("nil sink err = %v, want ErrCorrectionInvalid", err)
	}
}
