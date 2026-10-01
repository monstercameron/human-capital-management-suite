package journeyclient

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func uxblindSTimelineDecisionTitle(locale, title string) (string, bool) {
	copy := productui.ResolveProductLocale(locale)
	for _, review := range []struct{ prefix, key string }{
		{"Finance review", "journey.timeline_review_finance"},
		{"Manager review", "journey.timeline_review_manager"},
		{"Reapproval", "journey.timeline_review_reapproval"},
		{"Approval", "journey.timeline_review_approval"},
	} {
		for _, decision := range []struct{ suffix, key string }{
			{" approved", "journey.timeline_decision_approved"},
			{" rejected", "journey.timeline_decision_rejected"},
		} {
			if title == review.prefix+decision.suffix {
				return copy.Text("journey.timeline_review_state", map[string]string{
					"review": copy.Text(review.key), "state": copy.Text(decision.key),
				}), true
			}
		}
	}
	return "", false
}

func uxblindSTimelineDetailLocale(locale, detail string) string {
	copy := productui.ResolveProductLocale(locale)
	switch {
	case strings.HasPrefix(detail, "assigned_to:"):
		return copy.Text("journey.timeline_assigned_to", map[string]string{
			"assignee": strings.TrimSpace(strings.TrimPrefix(detail, "assigned_to:")),
		})
	case strings.HasPrefix(detail, "decision_reason:"):
		return copy.Text("journey.timeline_decision_reason", map[string]string{
			"reason": strings.TrimSpace(strings.TrimPrefix(detail, "decision_reason:")),
		})
	default:
		return ""
	}
}
