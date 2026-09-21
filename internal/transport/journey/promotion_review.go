package journey

import (
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

// toPromotionReview projects the engine's REV-091-02 review onto the wire. It
// is a field copy: which manager is disclosed and whether pay is shown were
// decided by the engine under the viewer's authorization, and this function
// only refuses to widen that. An unavailable guardrail carries its status and
// reason and nothing else, so no amount can reach a viewer the engine did
// not authorize, even if a caller handed this function a malformed value.
func toPromotionReview(r *workspace.JourneyPromotionReview) *journeyv1.JourneyPromotionReview {
	if r == nil {
		return nil
	}
	out := &journeyv1.JourneyPromotionReview{
		CompensationGuardrail:  toCompensationGuardrail(r.Guardrail),
		ManagerUnchanged:       r.ManagerUnchanged,
		CurrentPositionTitle:   r.CurrentPositionTitle,
		TargetPositionTitle:    r.TargetPositionTitle,
		TargetOrganizationName: r.TargetOrganizationName,
	}
	if r.ManagerDisclosed() {
		out.TargetManagerEvaluated = true
		out.TargetManagerDisplayName = r.TargetManagerName
		out.AffectedDirectReports = int32(len(r.Impact.AffectedDirectReports))
		out.CycleSafe = r.Impact.CycleSafe
	}
	return out
}

func toCompensationGuardrail(g promotion.CompensationGuardrail) *journeyv1.JourneyCompensationGuardrail {
	if !g.Available() {
		return &journeyv1.JourneyCompensationGuardrail{
			Status:            journeyv1.JourneyCompensationGuardrail_STATUS_UNAVAILABLE,
			UnavailableReason: string(g.Reason),
		}
	}
	return &journeyv1.JourneyCompensationGuardrail{
		Status:                    journeyv1.JourneyCompensationGuardrail_STATUS_AVAILABLE,
		CurrentAnnualized:         g.CurrentAnnualized.Amount().String(),
		MinimumAnnualized:         g.MinimumAnnualized.Amount().String(),
		MaximumAnnualized:         g.MaximumAnnualized.Amount().String(),
		PermittedIncreaseFraction: g.PermittedIncreasePercent.Fraction().String(),
		BandPosition:              g.BandPosition.String(),
		Currency:                  g.Currency,
		EffectiveDateBasis:        g.EffectiveDateBasis.String(),
	}
}
