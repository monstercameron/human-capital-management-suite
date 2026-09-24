package productui

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/benefits"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func benefitsSurfaceDate(t *testing.T, text string) values.LocalDate {
	t.Helper()
	date, err := values.ParseLocalDate(text)
	if err != nil {
		t.Fatal(err)
	}
	return date
}

func benefitsSurfaceInput(t *testing.T) BenefitOptionInput {
	t.Helper()
	tenant := values.TenantId("tenant-benefits-ui")
	plan := values.EntityRef{Tenant: tenant, Kind: "benefit_plan", Id: "b6e1115e-3e38-4b5a-bae4-093b77b8b018"}
	revisionID := values.EntityRef{Tenant: tenant, Kind: "benefit_plan_revision", Id: "927a88ef-f3fa-4f8d-9cf8-ddd60feff6d0"}
	revisionToken, err := values.NewSequenceRevision("benefits.plan", 1)
	if err != nil {
		t.Fatal(err)
	}
	dateStart := benefitsSurfaceDate(t, "2026-01-01")
	dateEnd := benefitsSurfaceDate(t, "2027-01-01")
	interval, err := values.NewLocalDateInterval(dateStart, dateEnd, values.CalendarRef{Ref: "us-federal", Version: "2026.1"})
	if err != nil {
		t.Fatal(err)
	}
	windowStart := benefitsSurfaceDate(t, "2026-11-01")
	windowEnd := benefitsSurfaceDate(t, "2026-12-01")
	windowInterval, err := values.NewLocalDateInterval(windowStart, windowEnd, values.CalendarRef{Ref: "us-federal", Version: "2026.1"})
	if err != nil {
		t.Fatal(err)
	}
	window, err := benefits.NewEnrollmentWindowSet([]benefits.EnrollmentWindow{{ID: "oe-2026", Kind: benefits.WindowOpenEnrollment, PlanID: plan, PlanRevision: revisionToken.String(), Interval: windowInterval, Reason: "annual election"}})
	if err != nil {
		t.Fatal(err)
	}
	return BenefitOptionInput{
		Revision: benefits.PlanRevision{RevisionID: revisionID, PlanID: plan, Revision: revisionToken,
			PlanYear: benefits.PlanYear{PlanID: plan, Year: 2026, Effective: interval},
			Name:     "Medical PPO", Jurisdiction: "US-NY", Currency: "USD", CoverageTiers: []string{"employee", "family"}, Options: []string{"HSA", "telehealth"}, Effective: interval},
		Worker:           benefits.WorkerFacts{Tenant: string(tenant), WorkerRef: "worker-1", PlanRef: plan.String(), PopulationRef: "population-full-time", AsOf: time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC), HoursPerWeek: values.MustDecimal("40", 2, values.RoundingHalfUp), HoursSet: true, Classification: "FULL_TIME", Jurisdiction: "US-NY"},
		EligibilityRules: []benefits.EligibilityRule{{ID: "rule-medical", Version: "3", Tenant: string(tenant), MinHoursPerWeek: values.MustDecimal("30", 2, values.RoundingHalfUp), AllowedClassifications: []string{"FULL_TIME"}, Jurisdictions: []string{"US-NY"}, EffectiveFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), EffectiveTo: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)}},
		EnrollmentSet:    window, WindowKind: benefits.WindowOpenEnrollment, ElectionDate: benefitsSurfaceDate(t, "2026-11-15"),
	}
}

func TestTodo_REV_074_03(t *testing.T) {
	input := benefitsSurfaceInput(t)
	got, err := ResolveBenefitOptions([]BenefitOptionInput{input})
	if err != nil {
		t.Fatalf("ResolveBenefitOptions: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("options = %d, want 1", len(got))
	}
	option := got[0]
	if option.PlanName != "Medical PPO" || option.PlanYear != 2026 || option.PlanRevision != input.Revision.Revision.String() {
		t.Fatalf("plan identity was not projected from the pinned revision: %+v", option)
	}
	if option.Eligibility.Status != benefits.StatusEligible || option.Window.Status != benefits.WindowOpen || !option.CanElect {
		t.Fatalf("eligible worker in an open window cannot elect: %+v", option)
	}
	if len(option.CoverageTiers) != 2 || option.CoverageTiers[1] != "family" || len(option.Options) != 2 {
		t.Fatalf("plan choices were not projected: %+v", option)
	}
	option.CoverageTiers[0] = "tampered"
	option.Options[0] = "tampered"
	if input.Revision.CoverageTiers[0] != "employee" || input.Revision.Options[0] != "HSA" {
		t.Fatal("projection aliases mutable source slices")
	}

	t.Run("conditional or closed elections are never enabled", func(t *testing.T) {
		conditional := benefitsSurfaceInput(t)
		conditional.Worker.HoursSet = false
		conditional.EnrollmentSet = benefits.EnrollmentWindowSet{Windows: conditional.EnrollmentSet.Windows}
		conditional.ElectionDate = benefitsSurfaceDate(t, "2026-12-01")
		conditionalResult, err := ResolveBenefitOptions([]BenefitOptionInput{conditional})
		if err != nil {
			t.Fatal(err)
		}
		if conditionalResult[0].Eligibility.Status != benefits.StatusConditional || conditionalResult[0].Window.Status != benefits.WindowClosed || conditionalResult[0].CanElect {
			t.Fatalf("uncertain or closed election became actionable: %+v", conditionalResult[0])
		}
	})

	t.Run("malformed domain inputs fail closed", func(t *testing.T) {
		bad := benefitsSurfaceInput(t)
		bad.EligibilityRules = nil
		if _, err := ResolveBenefitOptions([]BenefitOptionInput{bad}); err == nil {
			t.Fatal("missing eligibility rules were treated as an empty catalogue")
		}
	})

	t.Run("reconciliation is computed from carrier and payroll facts", func(t *testing.T) {
		now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
		effective := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
		cost := values.MustDecimal("25.00", 2, values.RoundingHalfUp)
		input := BenefitReconInput{
			Expectation: benefits.CoverageExpectation{Tenant: "tenant-benefits-ui", WorkerRef: "worker-1", ElectionDigest: "sha256:election", Tier: benefits.TierEmployeeOnly, EffectiveDate: effective, RateVersion: "rate-2026-v3", PerPeriodDeduction: cost},
			Carrier:     benefits.CarrierObservation{Tier: benefits.TierEmployeeOnly, EffectiveDate: effective, FreshAsOf: now},
			Payroll:     benefits.PayrollObservation{PerPeriodDeduction: cost, RateVersion: "rate-2026-v3", FreshAsOf: now},
			AsOf:        now,
		}
		matched, err := ResolveBenefitReconciliation([]BenefitReconInput{input})
		if err != nil {
			t.Fatal(err)
		}
		if len(matched) != 1 || matched[0].Outcome != benefits.ReconMatch || matched[0].Repair != nil || matched[0].Digest == "" {
			t.Fatalf("exact agreement did not produce a match: %+v", matched)
		}
		input.Payroll.Stale = true
		stale, err := ResolveBenefitReconciliation([]BenefitReconInput{input})
		if err != nil {
			t.Fatal(err)
		}
		if stale[0].Outcome != benefits.ReconStale || stale[0].Repair == nil || stale[0].Repair.Scope != "tenant-benefits-ui/worker-1" {
			t.Fatalf("stale provider state did not produce scoped repair: %+v", stale[0])
		}
	})
}

func TestTodo_REV_074_03_Golden(t *testing.T) {
	input := benefitsSurfaceInput(t)
	first, err := ResolveBenefitOptions([]BenefitOptionInput{input})
	if err != nil {
		t.Fatal(err)
	}
	second, err := ResolveBenefitOptions([]BenefitOptionInput{input})
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("projection lengths = %d and %d", len(first), len(second))
	}
	a, b := first[0], second[0]
	if a.PlanRef != b.PlanRef || a.PlanName != b.PlanName || a.PlanRevision != b.PlanRevision || a.PlanYear != b.PlanYear || a.Eligibility.Digest != b.Eligibility.Digest || a.Window.WindowID != b.Window.WindowID || a.Window.Status != b.Window.Status || a.CanElect != b.CanElect {
		t.Fatalf("same pinned domain records produced different projections:\nfirst: %+v\nsecond: %+v", a, b)
	}

	badWindow := benefitsSurfaceInput(t)
	badWindow.EnrollmentSet = benefits.EnrollmentWindowSet{}
	withoutPublishedWindow, err := ResolveBenefitOptions([]BenefitOptionInput{badWindow})
	if err != nil {
		t.Fatalf("an unpublished window should remain an explicit unknown state: %v", err)
	}
	if len(withoutPublishedWindow) != 1 || withoutPublishedWindow[0].Window.Status != benefits.WindowUnknown || withoutPublishedWindow[0].CanElect {
		t.Fatalf("unpublished window was not kept unknown and non-actionable: %+v", withoutPublishedWindow)
	}
}
