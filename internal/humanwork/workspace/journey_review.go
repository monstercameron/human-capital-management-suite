package workspace

import "github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"

// JourneyPromotionReview is REV-091-02's reviewer view of one promotion: the
// reporting-line impact PROMOUX-005 computes and the compensation guardrail
// PROMOUX-006 computes, carried as the domain's own typed values so no layer
// between the engine and the renderer can recompute either.
type JourneyPromotionReview struct {
	// Impact is the evaluated management impact. Its zero value means no
	// target manager was resolved and disclosed to this viewer.
	Impact promotion.ManagementImpact
	// TargetManagerName is the target manager's display name. It is set only
	// when Impact is evaluated, so an undisclosed manager is never named.
	TargetManagerName string
	// ManagerUnchanged is true when the request names no new manager, so the
	// worker keeps their current reporting line.
	ManagerUnchanged bool
	// Guardrail is the compensation guardrail. An unauthorized viewer
	// receives GuardrailStatusUnavailable with every amount zero.
	Guardrail promotion.CompensationGuardrail
	// CurrentPositionTitle, TargetPositionTitle and TargetOrganizationName
	// are presentation labels from the position directory; empty when they
	// could not be resolved. Nothing downstream prints a position identifier
	// in their place.
	CurrentPositionTitle   string
	TargetPositionTitle    string
	TargetOrganizationName string
}

// ManagerDisclosed reports whether the review names a target manager.
func (r JourneyPromotionReview) ManagerDisclosed() bool { return r.Impact.Evaluated() }
