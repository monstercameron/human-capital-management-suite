package journeyclient

import (
	"strings"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
)

func TestTodo_UXBLIND_010(t *testing.T) {
	journey := &journeyv1.Journey{
		Current:     &journeyv1.Placement{JobCode: "PPL-HRBP3", Grade: "P4"},
		Target:      &journeyv1.Placement{JobCode: "PPL-HRBP4", Grade: "P5"},
		CurrentBase: "132000.00", ProposedBase: "160000.00", Currency: "USD",
		CurrentPayBasis: "ANNUAL_SALARY", ProposedPayBasis: "ANNUAL_SALARY",
		BusinessReason: "Expanded enterprise responsibilities.",
	}
	card := card(testConfig(), journey)
	for _, want := range []string{"Senior People Partner", "PPL-HRBP3", "Principal People Partner", "PPL-HRBP4", "USD 132,000.00 per year", "USD 160,000.00 per year", "Expanded enterprise responsibilities."} {
		// UXBLIND-095: codes and the business reason are separate fields, not bracketed into the headline.
		if !strings.Contains(card.Headline+" "+card.PlacementCodes+" "+card.PayLine+" "+card.BusinessReason, want) {
			t.Fatalf("summary omitted %q: headline=%q pay=%q", want, card.Headline, card.PayLine)
		}
	}
}

func TestTodo_UXBLIND_010_Browser(t *testing.T) {
	// The native projection is the component-level browser proof for this
	// package: the live browser only mounts the strings this function returns.
	journey := &journeyv1.Journey{Current: &journeyv1.Placement{JobCode: "IR-JCP", Grade: "C3"}, Target: &journeyv1.Placement{JobCode: "IR-FMN", Grade: "C4"}, CurrentBase: "34.50", ProposedBase: "40.00", Currency: "USD", CurrentPayBasis: payBasisHourly, ProposedPayBasis: payBasisHourly}
	if got := card(testConfig(), journey).PayLine; !strings.Contains(got, "USD 34.50 per hour") || !strings.Contains(got, "USD 40.00 per hour") {
		t.Fatalf("hourly summary = %q", got)
	}
}

func TestTodo_UXBLIND_010_Golden(t *testing.T) {
	journey := &journeyv1.Journey{Current: &journeyv1.Placement{JobCode: "PPL-HRBP3", Grade: "P4"}, Target: &journeyv1.Placement{JobCode: "PPL-HRBP4", Grade: "P5"}, CurrentBase: "132000", ProposedBase: "160000", Currency: "USD", BusinessReason: "Expanded scope"}
	card := card(testConfig(), journey)
	// UXBLIND-095: titles are the headline; codes and the reason are their own lines.
	got := card.Headline + "\n" + card.PlacementCodes + "\n" + card.PayLine + "\n" + card.BusinessReason
	want := "Senior People Partner (P4) → Principal People Partner (P5)\nPPL-HRBP3 → PPL-HRBP4\nUSD 132,000.00 per year → USD 160,000.00 per year (+21.2%)\nExpanded scope"
	if got != want {
		t.Fatalf("summary golden = %q, want %q", got, want)
	}
}

func TestTodo_UXBLIND_048(t *testing.T) {
	for _, stage := range []journeyv1.JourneyStage{journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL, journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL, journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL} {
		_, tone := StagePresentation(stage)
		if tone != toneInfo {
			t.Fatalf("stage %s tone=%q, want informational", stage, tone)
		}
	}
}

func TestTodo_UXBLIND_049(t *testing.T) {
	detail := &journeyv1.JourneyDetail{Journey: &journeyv1.Journey{CurrentBase: "34.50", ProposedBase: "40.00", Currency: "USD", ProposedPayBasis: payBasisHourly, EffectiveDate: "2026-07-01"}, PromotionReview: &journeyv1.JourneyPromotionReview{CompensationGuardrail: &journeyv1.JourneyCompensationGuardrail{CurrentAnnualized: "71760.00"}}}
	facts := financeCostFacts("en-US", detail)
	joined := ""
	for _, fact := range facts {
		joined += fact.Label + "=" + fact.Value + "\n"
	}
	for _, want := range []string{"Annualized cost increase", "Increase in this calendar year", "Budget line"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("finance facts omitted %q: %s", want, joined)
		}
	}
}
