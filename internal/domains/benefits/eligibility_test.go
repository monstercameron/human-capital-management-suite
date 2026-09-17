package benefits

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func dec(t *testing.T, s string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(s, 2, values.RoundingHalfUp)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func eligibilityFacts() WorkerFacts {
	return WorkerFacts{
		Tenant:         "acme",
		WorkerRef:      "worker-1",
		PlanRef:        "plan:medical/v3",
		PopulationRef:  "population:fulltime-us",
		AsOf:           time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC),
		HoursPerWeek:   values.MustDecimal("40", 2, values.RoundingHalfUp),
		HoursSet:       true,
		Classification: "FULL_TIME",
		Jurisdiction:   "US-CA",
	}
}

func eligibilityRules() []EligibilityRule {
	return []EligibilityRule{
		{
			ID: "rule:medical-base", Version: "v3", Tenant: "acme",
			MinHoursPerWeek:        values.MustDecimal("30", 2, values.RoundingHalfUp),
			AllowedClassifications: []string{"FULL_TIME", "PART_TIME"},
			Jurisdictions:          []string{"US-CA", "US-NY"},
			EffectiveFrom:          time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			EffectiveTo:            time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}
}

// TestTodo_BEN_003 is the primary BEN-003 contract test: exact
// worker/plan/date/population/rules input returns typed eligibility with
// evidence, while missing hours/classification/jurisdiction is
// conditional/unknown rather than silently eligible or ineligible.
func TestTodo_BEN_003(t *testing.T) {
	t.Run("exact facts return eligible with evidence", func(t *testing.T) {
		got, err := EvaluateEligibility(eligibilityFacts(), eligibilityRules())
		if err != nil {
			t.Fatalf("EvaluateEligibility: %v", err)
		}
		if got.Status != StatusEligible {
			t.Fatalf("status = %v, want ELIGIBLE", got.Status)
		}
		if got.RuleID != "rule:medical-base" || got.RuleVersion != "v3" {
			t.Fatalf("evaluation must name its rule: %+v", got)
		}
		if got.Digest == "" || len(got.Evidence) == 0 {
			t.Fatalf("evaluation must carry evidence and a digest: %+v", got)
		}
	})

	t.Run("missing hours is conditional", func(t *testing.T) {
		facts := eligibilityFacts()
		facts.HoursSet = false
		got, err := EvaluateEligibility(facts, eligibilityRules())
		if err != nil {
			t.Fatalf("EvaluateEligibility: %v", err)
		}
		if got.Status != StatusConditional {
			t.Fatalf("status = %v, want CONDITIONAL", got.Status)
		}
	})

	t.Run("missing classification is conditional", func(t *testing.T) {
		facts := eligibilityFacts()
		facts.Classification = ""
		got, err := EvaluateEligibility(facts, eligibilityRules())
		if err != nil {
			t.Fatalf("EvaluateEligibility: %v", err)
		}
		if got.Status != StatusConditional {
			t.Fatalf("status = %v, want CONDITIONAL", got.Status)
		}
	})

	t.Run("missing jurisdiction is unknown", func(t *testing.T) {
		facts := eligibilityFacts()
		facts.Jurisdiction = ""
		got, err := EvaluateEligibility(facts, eligibilityRules())
		if err != nil {
			t.Fatalf("EvaluateEligibility: %v", err)
		}
		if got.Status != StatusUnknown {
			t.Fatalf("status = %v, want UNKNOWN", got.Status)
		}
	})

	t.Run("unmet thresholds are ineligible with reasons", func(t *testing.T) {
		facts := eligibilityFacts()
		facts.HoursPerWeek = dec(t, "20")
		facts.Classification = "INTERN"
		got, err := EvaluateEligibility(facts, eligibilityRules())
		if err != nil {
			t.Fatalf("EvaluateEligibility: %v", err)
		}
		if got.Status != StatusIneligible {
			t.Fatalf("status = %v, want INELIGIBLE", got.Status)
		}
		if len(got.Reasons) < 2 {
			t.Fatalf("ineligibility must explain every failed rule: %+v", got)
		}
	})

	t.Run("no applicable rule is unknown, never eligible", func(t *testing.T) {
		facts := eligibilityFacts()
		facts.AsOf = time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC)
		got, err := EvaluateEligibility(facts, eligibilityRules())
		if err != nil {
			t.Fatalf("EvaluateEligibility: %v", err)
		}
		if got.Status != StatusUnknown {
			t.Fatalf("status = %v, want UNKNOWN", got.Status)
		}
	})

	t.Run("missing identity is an input error", func(t *testing.T) {
		for name, mutate := range map[string]func(*WorkerFacts){
			"tenant": func(f *WorkerFacts) { f.Tenant = "" },
			"worker": func(f *WorkerFacts) { f.WorkerRef = "" },
			"plan":   func(f *WorkerFacts) { f.PlanRef = "" },
			"as-of":  func(f *WorkerFacts) { f.AsOf = time.Time{} },
		} {
			facts := eligibilityFacts()
			mutate(&facts)
			if _, err := EvaluateEligibility(facts, eligibilityRules()); err == nil {
				t.Fatalf("%s: expected input error", name)
			}
		}
		if _, err := EvaluateEligibility(eligibilityFacts(), nil); err == nil {
			t.Fatal("nil rules: expected input error")
		}
	})
}

// TestTodo_BEN_003_Property holds the BEN-003 invariants: eligibility is
// pure and stable, ELIGIBLE requires every fact present, and the status
// vocabulary stays closed.
func TestTodo_BEN_003_Property(t *testing.T) {
	t.Run("eligible implies complete facts", func(t *testing.T) {
		base := eligibilityFacts()
		rules := eligibilityRules()
		for _, tc := range []struct {
			name   string
			mutate func(*WorkerFacts)
		}{
			{"no hours", func(f *WorkerFacts) { f.HoursSet = false }},
			{"no classification", func(f *WorkerFacts) { f.Classification = "" }},
			{"no jurisdiction", func(f *WorkerFacts) { f.Jurisdiction = "" }},
		} {
			facts := base
			tc.mutate(&facts)
			got, err := EvaluateEligibility(facts, rules)
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if got.Status == StatusEligible {
				t.Fatalf("%s: incomplete facts must never be ELIGIBLE", tc.name)
			}
		}
	})

	t.Run("evaluation is pure with a stable digest", func(t *testing.T) {
		first, err := EvaluateEligibility(eligibilityFacts(), eligibilityRules())
		if err != nil {
			t.Fatal(err)
		}
		second, err := EvaluateEligibility(eligibilityFacts(), eligibilityRules())
		if err != nil {
			t.Fatal(err)
		}
		if first.Digest != second.Digest || first.Status != second.Status {
			t.Fatal("same input must evaluate identically")
		}
	})

	t.Run("status vocabulary is closed", func(t *testing.T) {
		for _, s := range []EligibilityStatus{StatusEligible, StatusConditional, StatusIneligible, StatusUnknown} {
			if !s.Valid() {
				t.Fatalf("declared status %q must be valid", s)
			}
		}
		if (EligibilityStatus("SORT_OF")).Valid() {
			t.Fatal("undeclared status must be invalid")
		}
	})
}

// TestTodo_BEN_003_Security proves tenant isolation: a rule from another
// tenant can never make a worker eligible, and the failure names the fence.
func TestTodo_BEN_003_Security(t *testing.T) {
	rules := eligibilityRules()
	rules[0].Tenant = "other"
	_, err := EvaluateEligibility(eligibilityFacts(), rules)
	if err == nil {
		t.Fatal("cross-tenant rule must be refused")
	}
}
