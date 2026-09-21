package journeyclient

// REV-091-02: the detail page renders PROMOUX-005's reporting-line review and
// PROMOUX-006's compensation guardrail from the server's own projection
// (JourneyDetail.promotion_review). This file only converts the wire's exact
// decimal text back into the domain's value types and hands them to the
// existing productui adapters; it computes no range, percentage or cycle
// verdict, and a value it cannot parse fails closed to the unavailable card
// rather than rendering a partial one.

import (
	"strings"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/payband"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	journey "github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

// reviewCards projects the server's review onto the two existing cards. It
// returns nil when the server sent no review.
func reviewCards(locale string, detail *journeyv1.JourneyDetail) *journey.ReviewCards {
	review := detail.GetPromotionReview()
	if review == nil {
		return nil
	}
	copy := productui.ResolveProductLocale(locale)
	target := detail.GetJourney().GetTarget()
	// The adapter is handed the directory's names, never identifiers: an
	// organization falls back to its readable code, and a position the
	// directory could not name is left out rather than printed as an id.
	organization := strings.TrimSpace(review.GetTargetOrganizationName())
	if organization == "" {
		organization = productui.DisplayLabel(target.GetOrgUnit())
	}
	positionTitle := strings.TrimSpace(review.GetTargetPositionTitle())
	props := productui.PromotionReviewPropsFrom(copy, promotion.TargetPlacement{
		OrgUnit:    organization,
		PositionID: positionTitle,
	}, promotion.ManagementImpact{})
	if organization == "" {
		props.Organization = ""
	}
	if positionTitle == "" {
		props.Position = ""
	}
	props.ManagerUnchanged = review.GetManagerUnchanged()
	if name := strings.TrimSpace(review.GetTargetManagerDisplayName()); review.GetTargetManagerEvaluated() && name != "" {
		// The adapter names the manager by the reference it is given; the
		// wire carries the disclosed display name instead of an identifier,
		// so the same message keys are filled with that name here.
		key := "promotion_review.target_manager"
		if props.ManagerUnchanged {
			key = "promotion_review.manager_unchanged_named"
		}
		props.HasTargetManager = true
		props.TargetManager = copy.Text(key, map[string]string{"manager": name})
		props.AffectedDirectReportsCount = int(review.GetAffectedDirectReports())
		props.CycleSafe = review.GetCycleSafe()
	}
	wire := review.GetCompensationGuardrail()
	guardrail := productui.CompensationGuardrailReviewPropsFrom(copy, guardrailFromWire(wire))
	// The range date reads in the page's own date style, like every other
	// date on the detail page.
	if basis := formatDateLocale(locale, wire.GetEffectiveDateBasis()); guardrail.Available && basis != "" {
		label := copy.Text("compensation_guardrail.effective_date_basis")
		for i := range guardrail.Facts {
			if guardrail.Facts[i].Label == label {
				guardrail.Facts[i].Value = basis
			}
		}
	}
	return &journey.ReviewCards{Promotion: &props, Guardrail: &guardrail}
}

// reviewComparison names the positions in the current-versus-proposed table
// by their directory titles. A position the server could not name is shown
// as a dash when its value is a bare identifier, so the table never prints
// a UUID (UXLIVE-003); the row still reports whether the position changed.
func reviewComparison(locale string, rows []journey.ComparisonRow, detail *journeyv1.JourneyDetail) []journey.ComparisonRow {
	label := productui.ResolveProductLocale(locale).Text("journey.compare_position")
	review := detail.GetPromotionReview()
	for i := range rows {
		if rows[i].Label != label {
			continue
		}
		rows[i].Current = positionCell(rows[i].Current, review.GetCurrentPositionTitle())
		rows[i].Proposed = positionCell(rows[i].Proposed, review.GetTargetPositionTitle())
	}
	return rows
}

func positionCell(value, title string) string {
	if title = strings.TrimSpace(title); title != "" {
		return title
	}
	if looksLikeIdentifier(value) {
		return emDash
	}
	return value
}

// looksLikeIdentifier reports whether s is a canonical UUID, the shape every
// position identifier in this tenant takes.
func looksLikeIdentifier(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
				return false
			}
		}
	}
	return true
}

// guardrailFromWire rebuilds the domain guardrail from the wire. Anything but
// a complete, well-formed AVAILABLE message becomes the typed unavailable
// state, so a malformed or missing figure can never render as a range.
func guardrailFromWire(g *journeyv1.JourneyCompensationGuardrail) promotion.CompensationGuardrail {
	withheld := promotion.CompensationGuardrail{
		Status: promotion.GuardrailStatusUnavailable,
		Reason: promotion.GuardrailUnavailableReason(g.GetUnavailableReason()),
	}
	if g.GetStatus() != journeyv1.JourneyCompensationGuardrail_STATUS_AVAILABLE {
		return withheld
	}
	withheld.Reason = ""
	currency := strings.TrimSpace(g.GetCurrency())
	current, okCurrent := exactMoney(g.GetCurrentAnnualized(), currency)
	minimum, okMinimum := exactMoney(g.GetMinimumAnnualized(), currency)
	maximum, okMaximum := exactMoney(g.GetMaximumAnnualized(), currency)
	fractionText := strings.TrimSpace(g.GetPermittedIncreaseFraction())
	permitted, err := values.NewPercentage(fractionText, decimalScale(fractionText), values.RoundingHalfEven)
	if !okCurrent || !okMinimum || !okMaximum || err != nil {
		return withheld
	}
	basis, err := values.ParseLocalDate(strings.TrimSpace(g.GetEffectiveDateBasis()))
	if err != nil {
		return withheld
	}
	return promotion.CompensationGuardrail{
		Status:                   promotion.GuardrailStatusAvailable,
		CurrentAnnualized:        current,
		MinimumAnnualized:        minimum,
		MaximumAnnualized:        maximum,
		PermittedIncreasePercent: permitted,
		BandPosition:             bandPlacement(g.GetBandPosition()),
		Currency:                 currency,
		EffectiveDateBasis:       basis,
	}
}

// exactMoney parses decimal text at exactly the scale it was written with,
// so no digit is rounded away or invented.
func exactMoney(text, currency string) (values.Money, bool) {
	text = strings.TrimSpace(text)
	if text == "" || currency == "" {
		return values.Money{}, false
	}
	m, err := values.NewMoney(text, currency, decimalScale(text), values.RoundingHalfEven)
	if err != nil {
		return values.Money{}, false
	}
	return m, true
}

// decimalScale is the number of fractional digits in decimal text.
func decimalScale(text string) int32 {
	if point := strings.IndexByte(text, '.'); point >= 0 {
		return int32(len(text) - point - 1)
	}
	return 0
}

// bandPlacement maps the wire token onto the engine's closed vocabulary; an
// unknown token stays unspecified and renders as "Not determined".
func bandPlacement(token string) payband.Placement {
	switch strings.TrimSpace(token) {
	case payband.PlacementBelowMinimum.String():
		return payband.PlacementBelowMinimum
	case payband.PlacementInBand.String():
		return payband.PlacementInBand
	case payband.PlacementAboveMaximum.String():
		return payband.PlacementAboveMaximum
	default:
		return payband.PlacementUnspecified
	}
}
