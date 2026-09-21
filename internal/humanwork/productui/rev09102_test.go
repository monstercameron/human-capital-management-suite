package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestPromotionReviewManagerUnchanged covers REV-091-02's additions to the
// PROMOUX-005 card: a promotion that names no new manager says the worker
// keeps their manager instead of reading as a missing selection, and an
// organization or position the caller could not name is left out rather than
// rendered as an empty label.
func TestPromotionReviewManagerUnchanged(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	render := func(props PromotionReviewProps) string {
		t.Helper()
		markup, err := ui.RenderToString(PromotionReview(locale, props))
		if err != nil {
			t.Fatal(err)
		}
		return markup
	}

	unchanged := render(PromotionReviewProps{ManagerUnchanged: true, Position: "Position: Care Team Lead"})
	if !strings.Contains(unchanged, "Keeps the current manager") || strings.Contains(unchanged, "No target manager selected") {
		t.Fatalf("unchanged manager rendered:\n%s", unchanged)
	}
	if strings.Contains(unchanged, "promotion-review-organization") || !strings.Contains(unchanged, "Care Team Lead") {
		t.Fatalf("empty organization was rendered or the named position was dropped:\n%s", unchanged)
	}
	if strings.Contains(unchanged, "reporting-cycle") || strings.Contains(unchanged, "promotion-review-cycle") {
		t.Fatalf("an unevaluated manager rendered a cycle verdict:\n%s", unchanged)
	}

	named := render(PromotionReviewProps{ManagerUnchanged: true, HasTargetManager: true,
		TargetManager: locale.Text("promotion_review.manager_unchanged_named", map[string]string{"manager": "Dana Lee"}), CycleSafe: true})
	if !strings.Contains(named, "Keeps reporting to Dana Lee") || !strings.Contains(named, "promotion-review-cycle-safe") {
		t.Fatalf("evaluated unchanged manager rendered:\n%s", named)
	}
	if strings.Contains(named, "promotion-review-position") {
		t.Fatalf("empty position was rendered:\n%s", named)
	}

	// The library default without the flag is unchanged.
	if legacy := render(PromotionReviewProps{Organization: "Organization: X", Position: "Position: Y"}); !strings.Contains(legacy, "No target manager selected") {
		t.Fatalf("default copy changed:\n%s", legacy)
	}
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		if other.Text("promotion_review.manager_unchanged") == locale.Text("promotion_review.manager_unchanged") ||
			!strings.Contains(other.Text("promotion_review.manager_unchanged_named", map[string]string{"manager": "Dana"}), "Dana") {
			t.Fatalf("%s is missing its own manager-unchanged copy", code)
		}
	}
}

// TestCompensationGuardrailFactsAreADescriptionList proves the available
// card's label/value rows sit inside a <dl>, so they are pairs to assistive
// technology.
func TestCompensationGuardrailFactsAreADescriptionList(t *testing.T) {
	markup, err := ui.RenderToString(CompensationGuardrailCard(CompensationGuardrailProps{
		Title: "Compensation guardrail", Available: true,
		Facts: []FactProps{{Label: "Current pay", Value: "USD 1.00"}, {Label: "Maximum for this role", Value: "USD 2.00"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	dl := strings.Index(markup, `<dl class="compensation-guardrail-facts">`)
	dt := strings.Index(markup, "<dt")
	end := strings.Index(markup, "</dl>")
	if dl < 0 || dt < dl || end < strings.LastIndex(markup, "</dd>") {
		t.Fatalf("guardrail facts are not inside a description list:\n%s", markup)
	}
}
