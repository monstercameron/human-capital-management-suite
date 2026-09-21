package app

import (
	"github.com/monstercameron/human-capital-management-suite/internal/domains/performance"
)

// Promotion plan names for high-performer routing (HIPERF-004). These mirror
// internal/application's WorkflowPlanPrototype/WorkflowPlanExecute
// ("prototype"/"execute") as plain strings: the application owns the flag
// vocabulary and this package must not import the composition root back.
const (
	// PromotionPlanPrototype is the bounded approval graph. It never routes
	// to the variant.
	PromotionPlanPrototype = "prototype"
	// PromotionPlanExecute is the executable promotion graph. Only starts
	// under this plan consult the subject's calibrated rating.
	PromotionPlanExecute = "execute"
)

// PinsHighPerformerVariant reports whether a promotion start under plan for
// the subject holding rating must pin the high-performer variant digest
// rather than the plan's own digest. It is the eligibility predicate next to
// the plan resolver (executionStart carries Resolver): the journey body
// never decides the plan. Prototype mode never routes, an unrated subject
// never routes, and an invalid or forged rating never routes — resolution
// fails closed onto the plan's own digest.
func PinsHighPerformerVariant(plan string, rating *performance.CalibratedRating) bool {
	if plan != PromotionPlanExecute || rating == nil {
		return false
	}
	return performance.IsHighPerformer(*rating)
}

// ResolvePromotionPlanDigest selects the compiled-plan digest a promotion
// start resolves: the variant digest for a top-band subject under the
// execute plan, the plan's own digest for everyone else.
func ResolvePromotionPlanDigest(plan string, rating *performance.CalibratedRating, executeDigest, variantDigest string) string {
	if PinsHighPerformerVariant(plan, rating) {
		return variantDigest
	}
	return executeDigest
}
