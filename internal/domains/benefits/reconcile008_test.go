package benefits

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func ben008Expectation() CoverageExpectation {
	return CoverageExpectation{
		Tenant: "acme", WorkerRef: "worker-1", ElectionDigest: "sha256:election-head",
		Tier: TierFamily, Dependents: []string{"dep-child-1", "dep-spouse"},
		EffectiveDate:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		RateVersion:        "rates-2026.01",
		PerPeriodDeduction: values.MustDecimal("240.00", 2, values.RoundingHalfUp),
	}
}

func ben008Carrier() CarrierObservation {
	return CarrierObservation{
		Tier: TierFamily, Dependents: []string{"dep-spouse", "dep-child-1"},
		EffectiveDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		FreshAsOf:     time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC),
	}
}

func ben008Payroll() PayrollObservation {
	return PayrollObservation{
		PerPeriodDeduction: values.MustDecimal("240.00", 2, values.RoundingHalfUp),
		RateVersion:        "rates-2026.01",
		FreshAsOf:          time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC),
	}
}

var ben008Now = time.Date(2026, 3, 3, 0, 0, 0, 0, time.UTC)

// TestTodo_BEN_008 is the PRIMARY contract: expected election truth is
// compared against carrier and payroll observations; partial, stale or
// unknown observations create a repair.
func TestTodo_BEN_008(t *testing.T) {
	got, err := ReconcileCoverage(ben008Expectation(), ben008Carrier(), ben008Payroll(), ben008Now)
	if err != nil {
		t.Fatalf("ReconcileCoverage: %v", err)
	}
	if got.Outcome != ReconMatch || got.Repair != nil {
		t.Fatalf("exact agreement must MATCH with no repair: %+v", got)
	}
	if got.Digest == "" {
		t.Fatalf("result must seal a digest")
	}

	t.Run("payroll deduction drift is partial with repair", func(t *testing.T) {
		payroll := ben008Payroll()
		payroll.PerPeriodDeduction = values.MustDecimal("200.00", 2, values.RoundingHalfUp)
		got, err := ReconcileCoverage(ben008Expectation(), ben008Carrier(), payroll, ben008Now)
		if err != nil {
			t.Fatal(err)
		}
		if got.Outcome != ReconPartial || got.Repair == nil {
			t.Fatalf("deduction drift must be PARTIAL with repair: %+v", got)
		}
		if len(got.Repair.Fields) != 1 || got.Repair.Fields[0] != "payroll.deduction" {
			t.Fatalf("repair must scope the drifted field: %+v", got.Repair)
		}
	})

	t.Run("stale carrier creates repair, unknown stays unknown", func(t *testing.T) {
		carrier := ben008Carrier()
		carrier.Stale = true
		got, err := ReconcileCoverage(ben008Expectation(), carrier, ben008Payroll(), ben008Now)
		if err != nil {
			t.Fatal(err)
		}
		if got.Outcome != ReconStale || got.Repair == nil {
			t.Fatalf("stale carrier must be STALE with repair: %+v", got)
		}
		unknown := ben008Carrier()
		unknown.Unknown = true
		got, err = ReconcileCoverage(ben008Expectation(), unknown, ben008Payroll(), ben008Now)
		if err != nil {
			t.Fatal(err)
		}
		if got.Outcome != ReconUnknown || got.Repair == nil {
			t.Fatalf("unknown carrier must be UNKNOWN with repair: %+v", got)
		}
	})

	t.Run("broad divergence mismatches", func(t *testing.T) {
		carrier := ben008Carrier()
		carrier.Tier = TierEmployeeOnly
		carrier.Dependents = nil
		carrier.EffectiveDate = time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
		payroll := ben008Payroll()
		payroll.RateVersion = "rates-2025.12"
		got, err := ReconcileCoverage(ben008Expectation(), carrier, payroll, ben008Now)
		if err != nil {
			t.Fatal(err)
		}
		if got.Outcome != ReconMismatch || len(got.Repair.Fields) != 4 {
			t.Fatalf("broad divergence must MISMATCH with all fields: %+v", got)
		}
	})
}

func TestTodo_BEN_008_Property(t *testing.T) {
	a, err := ReconcileCoverage(ben008Expectation(), ben008Carrier(), ben008Payroll(), ben008Now)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ReconcileCoverage(ben008Expectation(), ben008Carrier(), ben008Payroll(), ben008Now)
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest != b.Digest {
		t.Fatalf("identical observations must reconcile identically")
	}
	// Dependent order is not semantic on either side.
	exp := ben008Expectation()
	exp.Dependents = []string{"dep-spouse", "dep-child-1"}
	got, err := ReconcileCoverage(exp, ben008Carrier(), ben008Payroll(), ben008Now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != ReconMatch {
		t.Fatalf("dependent order must not break MATCH: %+v", got)
	}
	// Widening divergence never returns to MATCH.
	payroll := ben008Payroll()
	payroll.PerPeriodDeduction = values.MustDecimal("0.00", 2, values.RoundingHalfUp)
	payroll.RateVersion = "rates-1999.01"
	got, err = ReconcileCoverage(ben008Expectation(), ben008Carrier(), payroll, ben008Now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome == ReconMatch || got.Repair == nil {
		t.Fatalf("divergence must not MATCH: %+v", got)
	}
	if _, err := ReconcileCoverage(CoverageExpectation{}, ben008Carrier(), ben008Payroll(), ben008Now); !errors.Is(err, ErrCoverageReconRejected) {
		t.Fatalf("missing expectation must be BEN_008_REJECTED")
	}
}

func TestTodo_BEN_008_Mutation(t *testing.T) {
	exp := ben008Expectation()
	exp.Tier = "FIRST_CLASS"
	if _, err := ReconcileCoverage(exp, ben008Carrier(), ben008Payroll(), ben008Now); !errors.Is(err, ErrCoverageReconRejected) {
		t.Fatalf("undeclared tier must be BEN_008_REJECTED")
	}
	bad := ben008Expectation()
	bad.PerPeriodDeduction = values.MustDecimal("240.00", 2, values.RoundingHalfUp)
	// Corrupt the decimal through an invalid scale path: zero time instead.
	if _, err := ReconcileCoverage(ben008Expectation(), ben008Carrier(), ben008Payroll(), time.Time{}); !errors.Is(err, ErrCoverageReconRejected) {
		t.Fatalf("missing instant must be BEN_008_REJECTED")
	}
	_ = bad
}
