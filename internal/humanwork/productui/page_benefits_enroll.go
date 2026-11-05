package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// benefitsEnrollPage is the route adapter for benefit enrollment. It
// resolves the server-authorized projection through the benefits domain and
// shapes the electable options for the enrollment feature component; the
// submitted choice is validated against that same resolution before it
// reaches the server adapter.
func benefitsEnrollPage(view View) ui.Node {
	projection := view.Benefits
	if projection.Elect == nil {
		return benefitsUnavailable(view, "benefits_enroll")
	}
	switch projection.State {
	case BenefitsReadNoRecord:
		return benefitsStateFallback(view, "benefits_enroll", "no_plans")
	case BenefitsReadError:
		return benefitsStateFallback(view, "benefits_enroll", "read_error")
	case BenefitsReadReady:
	default:
		return benefitsUnavailable(view, "benefits_enroll")
	}
	options, err := ResolveBenefitOptions(projection.Options)
	if err != nil {
		return benefitsStateFallback(view, "benefits_enroll", "read_error")
	}
	if electable := benefitElectableOptions(options); len(electable) > 0 {
		return benefitEnrollForm(view, projection, options, electable)
	}
	return benefitsStateFallback(view, "benefits_enroll", "no_plans")
}
