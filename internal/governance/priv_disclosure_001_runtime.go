package governance

import privacy "github.com/monstercameron/human-capital-management-suite/internal/governance/privacy"

// DiscloseAnalytics is the serving boundary for analytics results. Callers
// provide the budget owned by their process or tenant scope, so repeated
// queries remain cumulative without package-level mutable state.
func (PrivacyRuntime) DiscloseAnalytics(
	policy privacy.DisclosurePolicy,
	query privacy.AnalyticsQuery,
	cells []privacy.AnalyticsCell,
	budget *privacy.DisclosureBudget,
) (privacy.DisclosureResult, error) {
	return privacy.Apply(policy, query, cells, budget)
}
