package productui

import "fmt"

// AsyncRegionLayoutShiftBudget is the frontend engineering budget
// UXAUDIT-012's GREEN clause requires cumulative layout shift to stay
// within: "cumulative layout shift stays within the declared frontend
// budget."
//
// No governed, server-authoritative performance budget exists for this yet.
// PagePerformanceBudgets (page_performance_budgets.go), the registry surface
// WEB-236 built for exactly this concern, is deliberately an honest stub:
// "the governed performance service is not published to this UI yet ...
// budget truth stays server authority ... until then the UI will not
// simulate one," and WEB-236's own test forbids inventing figures such as
// "budget met [check]". This constant does not violate that: it is not
// presented as business-authoritative budget truth published by a governed
// service, the same way latencygate's hardcoded p95 budgets (e.g.
// TestTodo_WEB_039_InteractionP95's 5ms) are engineering test thresholds,
// not business data. 0.1 is the widely published Core Web Vitals "Good"
// threshold for Cumulative Layout Shift, used here as the frontend
// engineering budget until a governed figure exists to replace it.
const AsyncRegionLayoutShiftBudget = 0.1

// LayoutShiftScore computes a deterministic, Core-Web-Vitals-style shift
// score for one region's transition between a declared "before" height (for
// example, its loading skeleton's total declared pixel height) and an
// "after" height (its resolved content's total pixel height), given the
// viewport height it renders within. It applies the Web Vitals impact
// fraction x distance fraction formula specialized to the shape every proxy
// in this package is built for: a region whose height changes while its
// position and width do not. That conservatively impacts every pixel below
// it in the viewport (impact fraction 1) and moves it by the height delta
// (distance fraction = |delta| / viewportHeightPx).
//
// heightBeforePx and heightAfterPx are real declared pixel heights, not
// "unknown" sentinels -- a caller that does not yet know a height should not
// call this function, the same way [RegionCursor] in
// internal/domains/promotion refuses to treat an unset sequence as caught
// up. viewportHeightPx must be positive.
func LayoutShiftScore(heightBeforePx, heightAfterPx, viewportHeightPx float64) (float64, error) {
	if viewportHeightPx <= 0 {
		return 0, fmt.Errorf("productui: layout shift viewport height must be positive, got %v", viewportHeightPx)
	}
	delta := heightAfterPx - heightBeforePx
	if delta < 0 {
		delta = -delta
	}
	return delta / viewportHeightPx, nil
}

// CheckLayoutShift reports whether a region transition's computed shift
// score stays within budget, returning the score alongside the error so
// callers can log it regardless of outcome.
func CheckLayoutShift(heightBeforePx, heightAfterPx, viewportHeightPx, budget float64) (float64, error) {
	score, err := LayoutShiftScore(heightBeforePx, heightAfterPx, viewportHeightPx)
	if err != nil {
		return 0, err
	}
	if score > budget {
		return score, fmt.Errorf("productui: layout shift score %v exceeds budget %v", score, budget)
	}
	return score, nil
}
