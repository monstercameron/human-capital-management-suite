package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// benefitsOverviewPage is the route adapter for the benefit-program
// overview. It resolves the server-authorized projection through the
// benefits domain and shapes it for the overview feature component; a
// subject with no eligible plans or a domain read failure keeps an honest
// empty state.
func benefitsOverviewPage(view View) ui.Node {
	projection := view.Benefits
	switch projection.State {
	case BenefitsReadNoRecord:
		return benefitsStateFallback(view, "benefits_overview", "no_plans")
	case BenefitsReadError:
		return benefitsStateFallback(view, "benefits_overview", "read_error")
	case BenefitsReadReady:
	default:
		return benefitsUnavailable(view, "benefits_overview")
	}
	options, err := ResolveBenefitOptions(projection.Options)
	if err != nil {
		return benefitsStateFallback(view, "benefits_overview", "read_error")
	}
	if len(options) == 0 {
		return benefitsStateFallback(view, "benefits_overview", "no_plans")
	}
	return benefitsOverviewContent(view, options)
}
