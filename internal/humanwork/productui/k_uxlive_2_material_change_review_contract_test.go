package productui

import (
	"strings"
	"testing"
)

// TestTodo_UXLIVE_034 compares the three established patterns and proves the
// selected contract covers the six promotion scenarios named by the todo.
func TestTodo_UXLIVE_034(t *testing.T) {
	contract := DefaultMaterialChangeReviewContract()
	if contract.SelectedPattern != "check-answers-plus-guarded-submit" {
		t.Fatalf("selected pattern = %q", contract.SelectedPattern)
	}
	if len(contract.Patterns) < 3 {
		t.Fatalf("review compared %d patterns, want at least three", len(contract.Patterns))
	}
	for _, pattern := range contract.Patterns {
		if pattern.Name == "" || pattern.Strength == "" || pattern.Limitation == "" {
			t.Fatalf("incomplete research comparison: %+v", pattern)
		}
	}
	want := []MaterialChangeScenario{
		MaterialChangeOrdinary, MaterialChangeAboveBand, MaterialChangeBudgetShortfall,
		MaterialChangeMissingRequired, MaterialChangeStaleBaseline, MaterialChangeMaskedPay,
	}
	seen := map[MaterialChangeScenario]bool{}
	for _, decision := range contract.Scenarios {
		seen[decision.Scenario] = true
		if decision.ReaderMessage == "" || len(decision.Facts) == 0 {
			t.Fatalf("scenario lacks a reader contract: %+v", decision)
		}
	}
	for _, scenario := range want {
		if !seen[scenario] {
			t.Fatalf("scenario %q is absent from the review contract", scenario)
		}
	}
}

// TestTodo_UXLIVE_034_Golden pins the reviewed progressive-disclosure and
// validation decision without submitting a promotion or depending on markup.
func TestTodo_UXLIVE_034_Golden(t *testing.T) {
	contract := DefaultMaterialChangeReviewContract()
	golden := strings.Join(append(append([]string{}, contract.ConfirmationContent...), contract.ValidationTiming, contract.WarningSeverity, contract.MobileInteraction), " ")
	for _, want := range []string{
		"required fields on blur and before review",
		"policy, authorization and stale-baseline checks on review and again on submit",
		"above-band, budget-shortfall and stale-baseline findings block submission",
		"employee identity and target role",
		"business reason",
		"progressive review opens as a focused full-width checkpoint",
	} {
		if !strings.Contains(golden, want) {
			t.Fatalf("review contract lost golden decision %q", want)
		}
	}
}

// TestTodo_UXLIVE_034_Accessibility pins the focus and naming contract for
// the eventual shared review surface.
func TestTodo_UXLIVE_034_Accessibility(t *testing.T) {
	contract := DefaultMaterialChangeReviewContract()
	if len(contract.AssistiveTechnology) != 3 {
		t.Fatalf("assistive-technology contract has %d rules, want three", len(contract.AssistiveTechnology))
	}
	joined := strings.Join(contract.AssistiveTechnology, " ")
	for _, want := range []string{"named heading", "focus", "final action", "go-back"} {
		if !strings.Contains(strings.ToLower(joined), want) {
			t.Fatalf("assistive-technology contract omits %q: %s", want, joined)
		}
	}
}

// TestTodo_UXLIVE_034_Security ensures masked compensation is represented as
// an unavailable fact and never as a redacted-looking number that could be
// mistaken for an authorized value.
func TestTodo_UXLIVE_034_Security(t *testing.T) {
	contract := DefaultMaterialChangeReviewContract()
	for _, decision := range contract.Scenarios {
		if decision.Scenario != MaterialChangeMaskedPay {
			continue
		}
		joined := strings.Join(decision.Facts, " ")
		if !strings.Contains(joined, "current compensation unavailable") || strings.Contains(joined, "USD") || strings.Contains(joined, "****") {
			t.Fatalf("masked compensation disclosure is unsafe: %+v", decision)
		}
		return
	}
	t.Fatal("masked-compensation scenario missing")
}

// TestTodo_UXLIVE_034_Conformance proves all blocking scenarios have a
// correction-oriented message and that only complete ordinary review data
// reaches the review checkpoint.
func TestTodo_UXLIVE_034_Conformance(t *testing.T) {
	contract := DefaultMaterialChangeReviewContract()
	for _, decision := range contract.Scenarios {
		if decision.Blocking && decision.ReaderMessage == "" {
			t.Fatalf("blocking scenario has no correction message: %+v", decision)
		}
		if decision.Scenario == MaterialChangeMissingRequired && decision.ReviewRequired {
			t.Fatal("missing required values may not open review")
		}
	}
}
