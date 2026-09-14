package main

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/productclient"
)

// keepResolvedPageDuringLoad retains the active content for an in-place
// refresh. A different journey is a different subject even though both URLs
// use the Journeys page, so its previous detail must not flash on the new URL.
func keepResolvedPageDuringLoad(lastPage, requestedPage productui.PageID, activeJourney, requestedJourney string) bool {
	if lastPage != requestedPage {
		return false
	}
	if requestedPage != productui.PageJourneys {
		return true
	}
	return activeJourney != "" && activeJourney == requestedJourney
}

// keepResolvedProductViewDuringLoad is the stronger form used by the product
// router. Same-page navigation is not necessarily an in-place refresh: Person
// and other subject-scoped routes can change identity while retaining the
// same page id. Keeping the prior view in that case would briefly paint the
// previous person's private workflows under the new address.
func keepResolvedProductViewDuringLoad(last productui.View, requested productclient.State, activeJourney, requestedJourney string) bool {
	if !keepResolvedPageDuringLoad(last.Page, requested.Page, activeJourney, requestedJourney) {
		return false
	}
	request := requested.Request
	trimmedEqual := func(a, b string) bool { return strings.TrimSpace(a) == strings.TrimSpace(b) }
	switch requested.Page {
	case productui.PagePerson:
		return trimmedEqual(last.SelectedPerson, request.SelectedPerson)
	case productui.PageWork:
		return trimmedEqual(last.SelectedWork, request.SelectedWork)
	case productui.PageHistory:
		// A history-person filter changes the subject-scoped result set. Keep
		// the old projection only when that filter remains the same.
		return trimmedEqual(last.HistoryPerson, request.HistoryPerson)
	case productui.PageOrganization, productui.PageOrgExplorer, productui.PageOrgOutline, productui.PageOrgResponsive:
		// Organization routes use the same page projection for an optional
		// person-focused view. An omitted person is meaningful too: it clears
		// the previous selection, so a warm render must not carry that person's
		// organization context into the new address.
		return trimmedEqual(last.SelectedPerson, request.SelectedPerson)
	default:
		return true
	}
}

// productBaselineForLoad returns the previously-authorized projection only
// when it is safe to use as a warm baseline.  A baseline is also used by the
// product client to seed page data, so carrying it across a destination or
// subject change can leave an omitted selector (for example /organization
// without person=) pointing at the old subject after ApplyRequest.  Clearing
// route-owned selections keeps the shell warm while making the next load
// start with no private subject context.
func productBaselineForLoad(last productui.View, requested productclient.State, activeJourney, requestedJourney string) productui.View {
	if keepResolvedProductViewDuringLoad(last, requested, activeJourney, requestedJourney) {
		return last
	}
	baseline := last
	baseline.SelectedPerson = ""
	baseline.SelectedWork = ""
	baseline.HistoryPerson = ""
	baseline.JourneyID = ""
	baseline.JourneyWorker = ""
	baseline.JourneyMode = ""
	return baseline
}
