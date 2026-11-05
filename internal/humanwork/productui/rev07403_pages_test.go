package productui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/benefits"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// RED for REV-074-03: the four benefits pages must evaluate the supplied
// domain facts instead of returning a static unavailable notice. A populated
// Benefits projection renders the worker's real plan options, coverage
// tiers, enrollment window, electability and reconciliation status; a
// missing projection, an empty catalogue, or a domain read failure keeps an
// honest empty state.
func rev07403ReadyView(t *testing.T, page PageID) View {
	t.Helper()
	view := testView(page)
	view.Benefits = BenefitsProjection{
		State:   BenefitsReadReady,
		Options: []BenefitOptionInput{benefitsSurfaceInput(t)},
		Recon: []BenefitReconInput{
			{
				Expectation: benefits.CoverageExpectation{Tenant: "tenant-benefits-ui", WorkerRef: "worker-1", ElectionDigest: "sha256:election", Tier: benefits.TierEmployeeOnly, EffectiveDate: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), RateVersion: "rate-2026-v3", PerPeriodDeduction: values.MustDecimal("25.00", 2, values.RoundingHalfUp)},
				Carrier:     benefits.CarrierObservation{Tier: benefits.TierEmployeeOnly, EffectiveDate: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), FreshAsOf: time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)},
				Payroll:     benefits.PayrollObservation{PerPeriodDeduction: values.MustDecimal("25.00", 2, values.RoundingHalfUp), RateVersion: "rate-2026-v3", FreshAsOf: time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)},
				AsOf:        time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC),
			},
			{
				Expectation: benefits.CoverageExpectation{Tenant: "tenant-benefits-ui", WorkerRef: "worker-1", ElectionDigest: "sha256:election-2", Tier: benefits.TierFamily, EffectiveDate: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), RateVersion: "rate-2026-v3", PerPeriodDeduction: values.MustDecimal("90.00", 2, values.RoundingHalfUp)},
				Carrier:     benefits.CarrierObservation{Tier: benefits.TierFamily, EffectiveDate: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), FreshAsOf: time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)},
				Payroll:     benefits.PayrollObservation{PerPeriodDeduction: values.MustDecimal("90.00", 2, values.RoundingHalfUp), RateVersion: "rate-2026-v3", FreshAsOf: time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC), Stale: true},
				AsOf:        time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC),
			},
		},
		Elect: func(BenefitElectionRequest, func(error)) {},
	}
	return view
}

func TestTodo_REV_074_03_Pages(t *testing.T) {
	t.Run("overview renders the resolved plan options", func(t *testing.T) {
		doc, err := Render(rev07403ReadyView(t, PageBenefitsOverview))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"Medical PPO", "2026", "employee", "family", "HSA", "Eligible", "Open"} {
			if !strings.Contains(doc, want) {
				t.Fatalf("benefits overview missing %q", want)
			}
		}
		if strings.Contains(doc, "⟦") {
			t.Fatal("benefits overview leaks an unresolved message key")
		}
	})

	t.Run("compare renders every option side by side", func(t *testing.T) {
		doc, err := Render(rev07403ReadyView(t, PageBenefitsCompare))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"Medical PPO", "family", "Eligible", "Open", "<table"} {
			if !strings.Contains(doc, want) {
				t.Fatalf("benefits compare missing %q", want)
			}
		}
	})

	t.Run("enroll offers only electable options through the domain adapter", func(t *testing.T) {
		doc, err := Render(rev07403ReadyView(t, PageBenefitsEnroll))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"Medical PPO", "EMPLOYEE_ONLY", "benefits-enroll-form", `id="benefits-enroll-plan"`, `id="benefits-enroll-tier"`, "Submit election"} {
			if !strings.Contains(doc, want) {
				t.Fatalf("benefits enroll missing %q", want)
			}
		}
	})

	t.Run("recon surfaces the real BEN-008 outcome", func(t *testing.T) {
		doc, err := Render(rev07403ReadyView(t, PagePayBenefitRecon))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"Match", "Stale", "tenant-benefits-ui/worker-1"} {
			if !strings.Contains(doc, want) {
				t.Fatalf("pay-benefit recon missing %q", want)
			}
		}
		if strings.Contains(doc, "sha256:election") {
			t.Fatal("pay-benefit recon prints the opaque election digest")
		}
	})

	t.Run("populated pages stay deterministic and key-tight in every locale", func(t *testing.T) {
		for _, page := range []PageID{PageBenefitsOverview, PageBenefitsCompare, PageBenefitsEnroll, PagePayBenefitRecon} {
			first, err := Render(rev07403ReadyView(t, page))
			if err != nil {
				t.Fatal(err)
			}
			second, err := Render(rev07403ReadyView(t, page))
			if err != nil {
				t.Fatal(err)
			}
			if first != second {
				t.Fatalf("%s renders nondeterministically", page)
			}
			for _, code := range SupportedProductLocales() {
				view := ApplyLocale(rev07403ReadyView(t, page), ResolveProductLocale(code))
				doc, err := Render(view)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(doc, "⟦") {
					t.Fatalf("%s leaks a key in %s", page, code)
				}
			}
		}
	})

	t.Run("empty catalogue and read failures keep honest empty states", func(t *testing.T) {
		empty := rev07403ReadyView(t, PageBenefitsOverview)
		empty.Benefits.Options = nil
		doc, err := Render(empty)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(doc, "No benefit plans are available") {
			t.Fatal("empty catalogue did not keep its empty state")
		}

		broken := rev07403ReadyView(t, PageBenefitsOverview)
		broken.Benefits.Options[0].EligibilityRules = nil
		doc, err = Render(broken)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(doc, "Benefit details could not be loaded") {
			t.Fatal("domain read failure did not fail closed")
		}

		ineligible := rev07403ReadyView(t, PageBenefitsEnroll)
		ineligible.Benefits.Options[0].Worker.HoursSet = false
		doc, err = Render(ineligible)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "benefits-enroll-form") || !strings.Contains(doc, "No benefit plans are available") {
			t.Fatalf("uncertain election stayed actionable")
		}
	})
}

func TestTodo_REV_074_03_EnrollSubmit(t *testing.T) {
	input := benefitsSurfaceInput(t)
	resolved, err := ResolveBenefitOptions([]BenefitOptionInput{input})
	if err != nil {
		t.Fatal(err)
	}
	projection := BenefitsProjection{State: BenefitsReadReady, Options: []BenefitOptionInput{input}}
	var captured *BenefitElectionRequest
	projection.Elect = func(req BenefitElectionRequest, done func(error)) {
		captured = &req
		done(nil)
	}
	valid := BenefitElectionRequest{
		PlanRef: resolved[0].PlanRef, PlanRevision: resolved[0].PlanRevision,
		WindowID: resolved[0].Window.WindowID, Tier: benefits.TierEmployeeOnly,
	}
	cases := []struct {
		name    string
		mutate  func(*BenefitElectionRequest)
		project func(*BenefitsProjection)
		want    bool
	}{
		{name: "valid election submits", mutate: func(*BenefitElectionRequest) {}, project: func(*BenefitsProjection) {}, want: true},
		{name: "unknown plan refused", mutate: func(r *BenefitElectionRequest) {
			r.PlanRef = "tenant-benefits-ui/benefit_plan/00000000-0000-4000-8000-000000000000"
		}, project: func(*BenefitsProjection) {}, want: false},
		{name: "foreign window refused", mutate: func(r *BenefitElectionRequest) { r.WindowID = "oe-1999" }, project: func(*BenefitsProjection) {}, want: false},
		{name: "unknown tier refused", mutate: func(r *BenefitElectionRequest) { r.Tier = "EVERYONE" }, project: func(*BenefitsProjection) {}, want: false},
		{name: "waiver without evidence refused", mutate: func(r *BenefitElectionRequest) { r.Waiver = true }, project: func(*BenefitsProjection) {}, want: false},
		{name: "missing adapter refused", mutate: func(*BenefitElectionRequest) {}, project: func(p *BenefitsProjection) { p.Elect = nil }, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := valid
			tc.mutate(&candidate)
			proj := projection
			tc.project(&proj)
			captured = nil
			if got := submitBenefitElection(proj, resolved, candidate, func(error) {}); got != tc.want {
				t.Fatalf("submit = %t, want %t", got, tc.want)
			}
			if tc.want && (captured == nil || *captured != candidate) {
				t.Fatalf("submitted request = %+v, want %+v", captured, candidate)
			}
			if !tc.want && captured != nil {
				t.Fatalf("refused election reached the adapter: %+v", captured)
			}
		})
	}

	t.Run("submitted request seals through BEN-005", func(t *testing.T) {
		mapped, err := valid.ToDomainInput(AuthorizedElectionContext{
			Tenant: "tenant-benefits-ui", WorkerRef: "worker-1", RateVersion: "rate-2026-v3",
			EmployeeCost: values.MustDecimal("25.00", 2, values.RoundingHalfUp), EmployerCost: values.MustDecimal("175.00", 2, values.RoundingHalfUp),
			EffectiveDate: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatal(err)
		}
		sealed, err := benefits.RecordElection(nil, mapped)
		if err != nil {
			t.Fatalf("RecordElection refused the mapped request: %v", err)
		}
		if sealed.Revision != 1 || sealed.CanonicalDigest == "" {
			t.Fatalf("genesis election was not sealed: %+v", sealed)
		}
		if err := sealed.Verify(); err != nil {
			t.Fatalf("sealed election does not verify: %v", err)
		}
		if _, err := valid.ToDomainInput(AuthorizedElectionContext{}); err == nil {
			t.Fatal("election mapped without server-authorized identity and costs")
		}
	})
}
