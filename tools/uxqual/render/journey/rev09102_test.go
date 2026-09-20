package journey

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestReviewCardsSection proves the detail page lays out productui's own
// review components under one labelled section, and draws nothing when the
// server sent no review.
func TestReviewCardsSection(t *testing.T) {
	if reviewCardsSectionLocale("en-US", nil) != nil || reviewCardsSectionLocale("en-US", &ReviewCards{}) != nil {
		t.Fatal("an absent review rendered a section")
	}
	promotion := productui.PromotionReviewProps{Organization: "Organization: Care Operations", HasTargetManager: true, TargetManager: "Reports to Dana Lee", CycleSafe: true}
	guardrail := productui.CompensationGuardrailProps{Title: "Compensation guardrail", Available: false,
		UnavailableAction: "Enter proposed pay", UnavailableMessage: "You are not authorized to view or set compensation for this promotion."}
	markup, err := ui.RenderToString(reviewCardsSectionLocale("en-US", &ReviewCards{Promotion: &promotion, Guardrail: &guardrail}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`aria-labelledby="review-checks-heading"`, `id="review-checks-heading"`, "Reporting line and pay range",
		`<h3 class="jn-subhead">Reporting line</h3>`, "Reports to Dana Lee", "Organization: Care Operations",
		"compensation-guardrail-unavailable", "not authorized",
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("review section missing %q:\n%s", want, markup)
		}
	}
	if strings.Count(markup, `class="jn-review-card"`) != 2 {
		t.Fatalf("want two review cards:\n%s", markup)
	}
	// Only the guardrail renders when the server sent no reporting line.
	only, err := ui.RenderToString(reviewCardsSectionLocale("de-DE", &ReviewCards{Guardrail: &guardrail}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(only, `class="jn-review-card"`) != 1 || !strings.Contains(only, "Berichtslinie und Gehaltsrahmen") {
		t.Fatalf("guardrail-only section:\n%s", only)
	}
}

// TestReviewCardsLayout pins the stylesheet rules the review cards depend
// on: one column by default (so a 390px viewport stacks them), two only from
// 48rem, facts in one column below 34rem, and the comparison table stacked on
// a phone and a table from 37.5rem.
func TestReviewCardsLayout(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		".jn-review-cards{display:grid;gap:var(--jn-s3);grid-template-columns:1fr;min-width:0;}",
		"@media (min-width:48rem){.jn-review-cards{grid-template-columns:1fr 1fr;}}",
		".compensation-guardrail-facts{column-gap:var(--jn-s2);display:grid;grid-template-columns:1fr;",
		"@media (min-width:34rem){.compensation-guardrail-facts{grid-template-columns:1fr 1fr;}}",
		".jn-review-card{", "overflow-wrap:anywhere", ".promotion-review-cycle-unsafe{color:var(--jn-warning)",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("stylesheet missing %q", want)
		}
	}
	// The comparison table stacks on a phone and is restored as a table by
	// a min-width breakpoint, never undone by a max-width one.
	for _, want := range []string{
		".jn-compare.jn-table{display:block;table-layout:auto;}",
		"@media (min-width:37.5rem){.jn-compare.jn-table{display:table;table-layout:fixed;}}",
		".jn-compare.jn-table>tbody>tr{",
		"flex-wrap:wrap",
		".jn-compare.jn-table>tbody>tr>td:nth-child(2),.jn-compare.jn-table>tbody>tr>td:nth-child(3){flex:0 0 auto;}",
		`.jn-compare.jn-table>tbody>tr>td.jn-proposed::before{`,
		"@media (min-width:37.5rem){.jn-compare.jn-table>tbody>tr>td.jn-proposed::before{content:none;}}",
		"@media (min-width:37.5rem){.jn-compare.jn-table>tbody>tr>:is(th,td){",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("stylesheet missing %q", want)
		}
	}
	if strings.Contains(buildTypedSheet(declareJourneyReview), "max-width") {
		t.Fatal("the review sheet undoes a layout with a max-width query")
	}
}
