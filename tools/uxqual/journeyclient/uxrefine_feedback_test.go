package journeyclient

import (
	"strings"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	journey "github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

// TestTodo_UXBLIND_048_Browser proves the projected waiting state and its
// cancellation explanation reach the same rendered page a browser mounts.
func TestTodo_UXBLIND_048_Browser(t *testing.T) {
	finance := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL)
	markup, err := journey.RenderToString(DetailPage(testConfig(), finance, nil, nil))
	if err != nil {
		t.Fatalf("RenderToString finance approval: %v", err)
	}
	if !strings.Contains(markup, `data-tone="info"`) {
		t.Fatalf("finance approval did not render an informational status tone:\n%s", markup)
	}
	if strings.Contains(markup, `jn-chip-icon`+`"`+`>`) && strings.Contains(markup, "Awaiting approval") {
		t.Fatalf("waiting approval markup still carries an icon-only warning affordance:\n%s", markup)
	}

	waiting := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE)
	waitingMarkup, err := journey.RenderToString(DetailPage(testConfig(), waiting, nil, nil))
	if err != nil {
		t.Fatalf("RenderToString waiting journey: %v", err)
	}
	if strings.Contains(waitingMarkup, "safe point") || strings.Contains(waitingMarkup, "engine") {
		t.Fatalf("cancellation explanation exposes implementation terminology:\n%s", waitingMarkup)
	}
	for _, want := range []string{"Request cancellation", "business", "promotion"} {
		if !strings.Contains(strings.ToLower(waitingMarkup), strings.ToLower(want)) {
			t.Fatalf("cancellation explanation omitted business wording %q:\n%s", want, waitingMarkup)
		}
	}
}

// TestTodo_UXBLIND_049_Browser proves the finance action is projected with
// the proposal's real pay, effective date, guardrail baseline and budget
// finding before the review surface is rendered.
func TestTodo_UXBLIND_049_Browser(t *testing.T) {
	detail := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL)
	detail.Journey.CurrentBase = "34.50"
	detail.Journey.ProposedBase = "40.00"
	detail.Journey.CurrentPayBasis = payBasisHourly
	detail.Journey.ProposedPayBasis = payBasisHourly
	detail.Journey.EffectiveDate = "2026-07-01"
	detail.PromotionReview = &journeyv1.JourneyPromotionReview{
		CompensationGuardrail: &journeyv1.JourneyCompensationGuardrail{CurrentAnnualized: "71760.00"},
	}

	page := DetailPage(testConfig(), detail, nil, nil)
	markup, err := journey.RenderToString(page)
	if err != nil {
		t.Fatalf("RenderToString finance review: %v", err)
	}
	for _, want := range []string{
		"Review and approve",
		"Confirm approval",
		"Annualized cost increase",
		"USD 11,440.00",
		"Increase in this calendar year",
		"USD 5,767.01",
		"Budget line",
		"The system checked the current budget baseline.",
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("finance confirmation omitted %q:\n%s", want, markup)
		}
	}
}
