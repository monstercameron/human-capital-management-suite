//go:build js && wasm

package main

import (
	"strings"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

// TestTodo_REV_091_02_Browser runs the production detail projection and the
// journey renderer inside the WASM build the browser loads: a detail whose
// server review carries a named manager and an available guardrail renders
// both productui cards with the server's figures and names, and prints no
// position identifier. The cards' layout rules are pinned natively by
// tools/uxqual/render/journey's TestReviewCardsLayout.
func TestTodo_REV_091_02_Browser(t *testing.T) {
	detail := &journeyv1.JourneyDetail{
		Journey: &journeyv1.Journey{
			IntentId: "01a0b9f3-e2f5-7a6e-8965-0eae328cf888", WorkerName: "Aya Tanaka", WorkerRef: "aya",
			Stage:       journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL,
			Current:     &journeyv1.Placement{JobCode: "CARE-2", Grade: "P2", OrgUnit: "care-operations", PositionId: "55555555-5555-4555-8555-555555555555"},
			Target:      &journeyv1.Placement{JobCode: "CARE-3", Grade: "P3", OrgUnit: "care-operations", PositionId: "66666666-6666-4666-8666-666666666666"},
			CurrentBase: "100000.00", ProposedBase: "115000.00", Currency: "USD", EffectiveDate: "2026-10-01",
		},
		PromotionReview: &journeyv1.JourneyPromotionReview{
			TargetManagerEvaluated: true, TargetManagerDisplayName: "Dana Lee", CycleSafe: true, ManagerUnchanged: true,
			TargetPositionTitle: "Care Team Lead", CurrentPositionTitle: "Care Coordinator", TargetOrganizationName: "Care Operations",
			CompensationGuardrail: &journeyv1.JourneyCompensationGuardrail{
				Status:            journeyv1.JourneyCompensationGuardrail_STATUS_AVAILABLE,
				CurrentAnnualized: "100000.00", MinimumAnnualized: "90000.00", MaximumAnnualized: "118000.00",
				PermittedIncreaseFraction: "0.180000", BandPosition: "IN_BAND", Currency: "USD", EffectiveDateBasis: "2026-10-01",
			},
		},
	}
	page := journeyclient.DetailPage(journeyclient.Config{Locale: "en-US"}, detail, nil, nil)
	// String rendering in the WASM build has no DOM adapter for event
	// handlers, so the interactive action and note controls are left out;
	// the review cards are static markup either way.
	page.Detail.Actions, page.Detail.Notes = nil, nil
	markup, err := journey.RenderToString(page)
	if err != nil {
		t.Fatalf("RenderToString under WASM: %v", err)
	}
	for _, want := range []string{
		`aria-labelledby="review-checks-heading"`, "Reporting line and pay range",
		`class="promotion-review"`, "Keeps reporting to Dana Lee", "Position: Care Team Lead",
		`class="compensation-guardrail compensation-guardrail-available"`, "118,000.00", "Largest raise the range allows",
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("WASM detail page missing %q", want)
		}
	}
	if strings.Contains(markup, "66666666-6666-4666-8666-666666666666") {
		t.Fatal("WASM detail page prints a position identifier")
	}
}
