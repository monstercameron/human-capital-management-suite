package app

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const reasonJourneySelfPromotion = "journey.feature_action.self_promotion"

func journeySelfPromotionError() error {
	return envelope.New(envelope.CodePermissionDenied, reasonJourneySelfPromotion,
		"you cannot propose a promotion for your own worker record").WithViolation(
		"worker_ref", "a promotion proposal must name another worker", reasonJourneySelfPromotion)
}

// journeySubjectIsViewer applies the same separation-of-duties rule to both
// corpus workers (stable keys) and durable workers (entity ids plus stable
// keys). Empty identifiers never match, so an incomplete principal cannot
// accidentally authorize a self-subject proposal.
func journeySubjectIsViewer(principal *trust.Principal, subject WorkerLocation) bool {
	if principal == nil {
		return false
	}
	viewer := strings.TrimSpace(principal.Subject())
	if viewer == "" {
		return false
	}
	return strings.EqualFold(viewer, strings.TrimSpace(subject.Key)) ||
		strings.EqualFold(viewer, strings.TrimSpace(subject.Ref.Id))
}
