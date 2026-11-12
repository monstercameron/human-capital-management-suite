package journeyclient

import (
	"strings"
	"testing"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
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
	facts := financeCostFacts("en-US", detail, financeTestNow)
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

// financeTestNow is the viewing date of the reported issue: the current
// calendar year is 2026 for every case below.
var financeTestNow = time.Date(2026, time.October, 23, 9, 0, 0, 0, time.UTC)

// financeFactValue returns the value of the fact with the given label, and
// whether the view carried one at all.
func financeFactValue(facts []journey.Fact, label string) (string, bool) {
	for _, fact := range facts {
		if fact.Label == label {
			return fact.Value, true
		}
	}
	return "", false
}

func TestTodo_UXBLIND_049_HourlyToHourlyAnnualizesBothSides(t *testing.T) {
	// The reported issue: 34.50/h -> 36.50/h from 2027-01-01 with no guardrail
	// baseline. The annual delta is 2.00 * 2080 = 4,160.00, not the
	// 75,885.50 that subtracted a raw hourly rate from an annualized one.
	detail := &journeyv1.JourneyDetail{Journey: &journeyv1.Journey{
		CurrentBase: "34.50", CurrentPayBasis: payBasisHourly,
		ProposedBase: "36.50", ProposedPayBasis: payBasisHourly,
		Currency: "USD", EffectiveDate: "2027-01-01",
	}}
	facts := financeCostFacts("en-US", detail, financeTestNow)
	if got, _ := financeFactValue(facts, "Annualized cost increase"); got != "USD\u00a04,160.00" {
		t.Fatalf("annualized cost increase = %q, want USD 4,160.00", got)
	}
	// A 2027 start contributes nothing to the 2026 calendar year.
	if got, ok := financeFactValue(facts, "Increase in this calendar year"); !ok || got != "USD\u00a00.00" {
		t.Fatalf("in-year increase = %q (present %v), want USD 0.00", got, ok)
	}
}

func TestTodo_UXBLIND_049_InYearProratesFromEffectiveDate(t *testing.T) {
	// 2026-07-01 through 2026-12-31 is 184 of 365 days: 4,160.00 * 184 / 365
	// is 2,097.10.
	detail := &journeyv1.JourneyDetail{Journey: &journeyv1.Journey{
		CurrentBase: "34.50", CurrentPayBasis: payBasisHourly,
		ProposedBase: "36.50", ProposedPayBasis: payBasisHourly,
		Currency: "USD", EffectiveDate: "2026-07-01",
	}}
	facts := financeCostFacts("en-US", detail, financeTestNow)
	if got, ok := financeFactValue(facts, "Increase in this calendar year"); !ok || got != "USD\u00a02,097.10" {
		t.Fatalf("in-year increase = %q (present %v), want USD 2,097.10", got, ok)
	}
}

func TestTodo_UXBLIND_049_EarlierEffectiveDateCountsFullYear(t *testing.T) {
	// An increase that took effect in an earlier year is in force for the whole
	// current calendar year.
	detail := &journeyv1.JourneyDetail{Journey: &journeyv1.Journey{
		CurrentBase: "34.50", CurrentPayBasis: payBasisHourly,
		ProposedBase: "36.50", ProposedPayBasis: payBasisHourly,
		Currency: "USD", EffectiveDate: "2025-03-01",
	}}
	facts := financeCostFacts("en-US", detail, financeTestNow)
	if got, ok := financeFactValue(facts, "Increase in this calendar year"); !ok || got != "USD\u00a04,160.00" {
		t.Fatalf("in-year increase = %q (present %v), want USD 4,160.00", got, ok)
	}
}

func TestTodo_UXBLIND_049_GuardrailBaselineStaysAnnual(t *testing.T) {
	// A guardrail's current figure is already annual, so an hourly proposal is
	// compared against it without a second annualization of the current side.
	detail := &journeyv1.JourneyDetail{Journey: &journeyv1.Journey{
		CurrentBase: "34.50", CurrentPayBasis: payBasisHourly,
		ProposedBase: "36.50", ProposedPayBasis: payBasisHourly,
		Currency: "USD", EffectiveDate: "2027-01-01",
	}, PromotionReview: &journeyv1.JourneyPromotionReview{CompensationGuardrail: &journeyv1.JourneyCompensationGuardrail{CurrentAnnualized: "71760.00"}}}
	facts := financeCostFacts("en-US", detail, financeTestNow)
	if got, _ := financeFactValue(facts, "Annualized cost increase"); got != "USD\u00a04,160.00" {
		t.Fatalf("annualized cost increase = %q, want USD 4,160.00 (75920.00 - 71760.00)", got)
	}
}

func TestTodo_UXBLIND_049_SalaryToSalary(t *testing.T) {
	detail := &journeyv1.JourneyDetail{Journey: &journeyv1.Journey{
		CurrentBase: "120000.00", CurrentPayBasis: "ANNUAL_SALARY",
		ProposedBase: "132000.00", ProposedPayBasis: "ANNUAL_SALARY",
		Currency: "USD", EffectiveDate: "2026-07-01",
	}}
	facts := financeCostFacts("en-US", detail, financeTestNow)
	if got, _ := financeFactValue(facts, "Annualized cost increase"); got != "USD\u00a012,000.00" {
		t.Fatalf("annualized cost increase = %q, want USD 12,000.00", got)
	}
	// 12,000.00 * 184 / 365 = 6,049.32 (rounded half away from zero).
	if got, ok := financeFactValue(facts, "Increase in this calendar year"); !ok || got != "USD\u00a06,049.32" {
		t.Fatalf("in-year increase = %q (present %v), want USD 6,049.32", got, ok)
	}
}

func TestTodo_UXBLIND_049_UnknownBasisIsNotSubtracted(t *testing.T) {
	// A basis the view cannot convert must not be subtracted from a salary:
	// the annualized cost reads "not available" rather than an annual delta,
	// and no in-year figure is derived from it.
	detail := &journeyv1.JourneyDetail{Journey: &journeyv1.Journey{
		CurrentBase: "900.00", CurrentPayBasis: "WEEKLY_RATE",
		ProposedBase: "950.00", ProposedPayBasis: payBasisHourly,
		Currency: "USD", EffectiveDate: "2027-01-01",
	}}
	// The cost lines say the amount is not available and the budget line stays
	// (#3636); no annual figure is derived from the unconvertible basis.
	facts := financeCostFacts("en-US", detail, financeTestNow)
	if got, _ := financeFactValue(facts, "Annualized cost increase"); strings.HasPrefix(got, "USD") {
		t.Fatalf("annualized cost from an unconvertible basis = %q, want no amount", got)
	}
	if got, _ := financeFactValue(facts, "Increase in this calendar year"); strings.HasPrefix(got, "USD") {
		t.Fatalf("in-year cost from an unconvertible basis = %q, want no amount", got)
	}
	if _, ok := financeFactValue(facts, "Budget line"); !ok {
		t.Fatalf("budget line dropped with the unconvertible basis: %v", facts)
	}
}
