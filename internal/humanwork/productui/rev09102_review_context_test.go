package productui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
)

// TestCompensationGuardrailReviewContext proves the review-page mode: an
// unavailable guardrail is one quiet, reason-specific sentence with no
// action and no figure, while the proposer form's card keeps its typed
// unavailable action.
func TestCompensationGuardrailReviewContext(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	render := func(props CompensationGuardrailProps) string {
		t.Helper()
		markup, err := ui.RenderToString(CompensationGuardrailCard(props))
		if err != nil {
			t.Fatal(err)
		}
		return markup
	}
	tags := regexp.MustCompile(`<[^>]*>`)
	digits := regexp.MustCompile(`[0-9]`)
	for reason, want := range map[promotion.GuardrailUnavailableReason]string{
		promotion.GuardrailReasonNotAuthorized:  "Pay details are not shown to you.",
		promotion.GuardrailReasonBandUnresolved: "No pay range is published for this role.",
		"":                                      "Compensation details are not available for this promotion.",
	} {
		g := promotion.CompensationGuardrail{Status: promotion.GuardrailStatusUnavailable, Reason: reason}
		review := render(CompensationGuardrailReviewPropsFrom(locale, g))
		if !strings.Contains(review, want) || strings.Contains(review, "<button") || strings.Contains(review, "Enter proposed pay") {
			t.Fatalf("%q review card:\n%s", reason, review)
		}
		if digits.MatchString(tags.ReplaceAllString(review, " ")) {
			t.Fatalf("%q review card renders a figure:\n%s", reason, review)
		}
		proposer := render(CompensationGuardrailPropsFrom(locale, g))
		if !strings.Contains(proposer, "<button") || !strings.Contains(proposer, "Enter proposed pay") {
			t.Fatalf("%q proposer card lost its typed unavailable action:\n%s", reason, proposer)
		}
	}
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		for _, key := range []string{"compensation_guardrail.review_unavailable.not_authorized", "compensation_guardrail.review_unavailable.band_unresolved"} {
			if other.Text(key) == locale.Text(key) {
				t.Fatalf("%s has no localized %s", code, key)
			}
		}
	}
}
