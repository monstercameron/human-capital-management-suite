package journeyclient

// REV-091-02: the live proposal/review page renders PROMOUX-005's
// reporting-line review and PROMOUX-006's compensation guardrail from the
// server's promotion_review, through productui's own components.
//
// TestTodo_REV_091_02 is the client half of the PRIMARY: a detail carrying a
// review renders both cards with the server's exact figures, names instead
// of identifiers, and the page's own date style. TestTodo_REV_091_02_Security
// proves a withheld or malformed guardrail renders no figure at all.

import (
	"regexp"
	"strings"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

const (
	rev09102CurrentPosition = "55555555-5555-4555-8555-555555555555"
	rev09102TargetPosition  = "66666666-6666-4666-8666-666666666666"
)

func rev09102Review() *journeyv1.JourneyPromotionReview {
	return &journeyv1.JourneyPromotionReview{
		TargetManagerEvaluated: true, TargetManagerDisplayName: "Dana Lee", CycleSafe: true, ManagerUnchanged: true,
		CurrentPositionTitle: "Care Coordinator", TargetPositionTitle: "Care Team Lead", TargetOrganizationName: "Care Operations",
		CompensationGuardrail: &journeyv1.JourneyCompensationGuardrail{
			Status:            journeyv1.JourneyCompensationGuardrail_STATUS_AVAILABLE,
			CurrentAnnualized: "100000.00", MinimumAnnualized: "90000.00", MaximumAnnualized: "118000.03",
			PermittedIncreaseFraction: "0.180000", BandPosition: "IN_BAND", Currency: "USD", EffectiveDateBasis: "2026-10-01",
		},
	}
}

func rev09102Detail(t *testing.T, review *journeyv1.JourneyPromotionReview) *journeyv1.JourneyDetail {
	t.Helper()
	detail := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL)
	detail.Journey.Current.PositionId = rev09102CurrentPosition
	detail.Journey.Target.PositionId = rev09102TargetPosition
	detail.PromotionReview = review
	return detail
}

func rev09102Render(t *testing.T, detail *journeyv1.JourneyDetail) (journey.Page, string) {
	t.Helper()
	page := DetailPage(testConfig(), detail, nil, nil)
	markup, err := journey.RenderToString(page)
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	return page, markup
}

// reviewSection cuts the review section out of the page markup.
func reviewSection(t *testing.T, markup string) string {
	t.Helper()
	start := strings.Index(markup, `class="jn-panel jn-review-checks"`)
	if start < 0 {
		t.Fatalf("page has no review section: %s", markup)
	}
	end := strings.Index(markup[start:], "</section>")
	if end < 0 {
		t.Fatal("review section is not closed")
	}
	return markup[start : start+end]
}

func TestTodo_REV_091_02(t *testing.T) {
	page, markup := rev09102Render(t, rev09102Detail(t, rev09102Review()))
	if page.Detail.Review == nil || page.Detail.Review.Promotion == nil || page.Detail.Review.Guardrail == nil {
		t.Fatalf("detail view carries no review cards: %+v", page.Detail.Review)
	}
	section := reviewSection(t, markup)
	for _, want := range []string{
		"Reporting line and pay range", "Reporting line",
		"Keeps reporting to Dana Lee", "Organization: Care Operations", "Position: Care Team Lead",
		"No reporting-cycle conflicts found",
		"Compensation guardrail", "Largest raise the range allows",
		"100,000.00", "90,000.00", "118,000.03", "18", "Within the range",
		formatDateLocale("en-US", "2026-10-01"),
		`class="compensation-guardrail-facts"`, `class="promotion-review"`,
	} {
		if !strings.Contains(section, want) {
			t.Fatalf("review section missing %q:\n%s", want, section)
		}
	}
	// The server's maximum is rendered, not base * 1.18 recomputed.
	if strings.Contains(section, "118,000.00") {
		t.Fatalf("review recomputed the maximum instead of rendering the server's:\n%s", section)
	}
	// The range date reads like every other date on the page.
	if strings.Contains(section, "10/01/2026") {
		t.Fatalf("range date uses a different format from the page:\n%s", section)
	}

	t.Run("positions are named, never printed as identifiers", func(t *testing.T) {
		for _, id := range []string{rev09102CurrentPosition, rev09102TargetPosition} {
			if strings.Contains(markup, id) {
				t.Fatalf("page prints the position identifier %s", id)
			}
		}
		for _, row := range page.Detail.Comparison {
			if row.Label == "Position" && (row.Current != "Care Coordinator" || row.Proposed != "Care Team Lead") {
				t.Fatalf("position row = %+v, want the directory titles", row)
			}
		}
		// An unnamed position is a dash in the table and absent from the card.
		unnamed := rev09102Review()
		unnamed.CurrentPositionTitle, unnamed.TargetPositionTitle = "", ""
		unnamedPage, unnamedMarkup := rev09102Render(t, rev09102Detail(t, unnamed))
		if strings.Contains(unnamedMarkup, rev09102TargetPosition) || strings.Contains(reviewSection(t, unnamedMarkup), "Position:") {
			t.Fatalf("an unnamed position leaked its identifier or an empty label:\n%s", reviewSection(t, unnamedMarkup))
		}
		for _, row := range unnamedPage.Detail.Comparison {
			if row.Label == "Position" && (row.Current != emDash || row.Proposed != emDash || !row.Changed) {
				t.Fatalf("unnamed position row = %+v, want dashes that still report the change", row)
			}
		}
	})

	t.Run("an unchanged reporting line says so rather than reading as an omission", func(t *testing.T) {
		unevaluated := rev09102Review()
		unevaluated.TargetManagerEvaluated, unevaluated.TargetManagerDisplayName = false, ""
		section := reviewSection(t, func() string { _, m := rev09102Render(t, rev09102Detail(t, unevaluated)); return m }())
		if !strings.Contains(section, "Keeps the current manager") || strings.Contains(section, "No target manager selected") ||
			strings.Contains(section, "reporting cycle") {
			t.Fatalf("unchanged, unevaluated manager rendered:\n%s", section)
		}
		named := rev09102Review()
		named.ManagerUnchanged = false
		section = reviewSection(t, func() string { _, m := rev09102Render(t, rev09102Detail(t, named)); return m }())
		if !strings.Contains(section, "Reports to Dana Lee") {
			t.Fatalf("a newly named manager rendered:\n%s", section)
		}
	})

	t.Run("no review from the server renders no section", func(t *testing.T) {
		page, markup := rev09102Render(t, rev09102Detail(t, nil))
		if page.Detail.Review != nil || strings.Contains(markup, "jn-review-checks") {
			t.Fatal("a detail without a review rendered the review section")
		}
	})
}

func TestTodo_REV_091_02_Security(t *testing.T) {
	digits := regexp.MustCompile(`[0-9]`)
	guardrailMarkup := func(t *testing.T, g *journeyv1.JourneyCompensationGuardrail) string {
		t.Helper()
		review := rev09102Review()
		review.CompensationGuardrail = g
		section := reviewSection(t, func() string { _, m := rev09102Render(t, rev09102Detail(t, review)); return m }())
		start := strings.Index(section, `compensation-guardrail `)
		if start < 0 {
			t.Fatalf("no guardrail card:\n%s", section)
		}
		return section[start:]
	}

	t.Run("a withheld guardrail renders its typed refusal and no figure", func(t *testing.T) {
		card := guardrailMarkup(t, &journeyv1.JourneyCompensationGuardrail{
			Status: journeyv1.JourneyCompensationGuardrail_STATUS_UNAVAILABLE, UnavailableReason: "NOT_AUTHORIZED",
		})
		if !strings.Contains(card, "compensation-guardrail-unavailable") || !strings.Contains(card, "Pay details are not shown to you.") {
			t.Fatalf("withheld card:\n%s", card)
		}
		// A reviewer gets an explanation, not the proposer's dead control.
		if strings.Contains(card, "<button") || strings.Contains(card, "Enter proposed pay") {
			t.Fatalf("withheld review card offers an action:\n%s", card)
		}
		if text := stripTags(card); digits.MatchString(text) || strings.Contains(text, "%") {
			t.Fatalf("withheld guardrail rendered a figure: %q", text)
		}
	})

	t.Run("a malformed available guardrail fails closed, never a partial range", func(t *testing.T) {
		for name, g := range map[string]*journeyv1.JourneyCompensationGuardrail{
			"missing maximum":  {Status: journeyv1.JourneyCompensationGuardrail_STATUS_AVAILABLE, CurrentAnnualized: "100000.00", MinimumAnnualized: "90000.00", PermittedIncreaseFraction: "0.18", Currency: "USD", EffectiveDateBasis: "2026-10-01"},
			"missing currency": {Status: journeyv1.JourneyCompensationGuardrail_STATUS_AVAILABLE, CurrentAnnualized: "100000.00", MinimumAnnualized: "90000.00", MaximumAnnualized: "118000.00", PermittedIncreaseFraction: "0.18", EffectiveDateBasis: "2026-10-01"},
			"bad date":         {Status: journeyv1.JourneyCompensationGuardrail_STATUS_AVAILABLE, CurrentAnnualized: "100000.00", MinimumAnnualized: "90000.00", MaximumAnnualized: "118000.00", PermittedIncreaseFraction: "0.18", Currency: "USD", EffectiveDateBasis: "soon"},
			"unset":            nil,
		} {
			card := guardrailMarkup(t, g)
			if !strings.Contains(card, "compensation-guardrail-unavailable") || strings.Contains(card, "<button") {
				t.Fatalf("%s: rendered as available or with an action:\n%s", name, card)
			}
			if text := stripTags(card); digits.MatchString(text) {
				t.Fatalf("%s: rendered a figure: %q", name, text)
			}
		}
	})
}

var rev09102Tags = regexp.MustCompile(`<[^>]*>`)

func stripTags(markup string) string { return rev09102Tags.ReplaceAllString(markup, " ") }
