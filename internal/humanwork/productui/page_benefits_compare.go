package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// benefitsComparePage is the route adapter for benefit-plan comparison. It
// resolves the server-authorized projection through the benefits domain and
// shapes it for the comparison feature component; an empty catalogue or a
// domain read failure keeps an honest empty state.
func benefitsComparePage(view View) ui.Node {
	projection := view.Benefits
	switch projection.State {
	case BenefitsReadNoRecord:
		return benefitsStateFallback(view, "benefits_compare", "no_plans")
	case BenefitsReadError:
		return benefitsStateFallback(view, "benefits_compare", "read_error")
	case BenefitsReadReady:
	default:
		return benefitsUnavailable(view, "benefits_compare")
	}
	options, err := ResolveBenefitOptions(projection.Options)
	if err != nil {
		return benefitsStateFallback(view, "benefits_compare", "read_error")
	}
	if len(options) == 0 {
		return benefitsStateFallback(view, "benefits_compare", "no_plans")
	}
	return benefitsCompareContent(view, options)
}
