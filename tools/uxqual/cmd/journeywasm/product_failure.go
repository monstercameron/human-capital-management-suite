package main

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// productLoadErrorMessage is the older page-level failure copy, kept for the
// pages that still degrade through View.LoadError.
const productLoadErrorMessage = "We couldn't load this page. Try again."

// productRouteFailure decides which failure contract a failed route read
// uses. A covered region (People, Work, History, Organization) fails through
// AsyncRegionFailure and must not also carry View.LoadError, which would
// stack the shell's differently sized panel over the region. Other pages
// keep their View.LoadError degradation.
func productRouteFailure(view productui.View, page productui.PageID, loadErr error) (productui.View, bool) {
	if loadErr == nil {
		return view, false
	}
	view.Page = page
	if view.SignedOut != nil {
		// An authentication refusal converges the shell to its sign-in
		// recovery panel; the route's ordinary retry region must not cover it.
		view.LoadError = ""
		return view, false
	}
	if productui.AsyncRegionFailureCovered(page) {
		view.LoadError = ""
		return view, true
	}
	view.LoadError = productLoadErrorMessage
	return view, false
}

// productRouteRetry re-runs the current route's read in place. The live
// router sets it to its Revalidate; it is nil without a mounted router, and
// the failure notice then offers the page's own address as a link.
var productRouteRetry func()

// productRetryFocusPending asks the next render of the same route to put
// focus back where the reader can act: the Retry control if the read failed
// again, otherwise the page heading.
var productRetryFocusPending bool

// retryProductRoute is the Retry control's action.
func retryProductRoute() {
	if productRouteRetry == nil {
		return
	}
	productRetryFocusPending = true
	productRouteRetry()
}

// productRouteContent renders the route outlet for a non-journey page.
func productRouteContent(view productui.View, failed bool) ui.Node {
	if failed {
		var retry func()
		if productRouteRetry != nil {
			retry = retryProductRoute
		}
		return productui.BuildContentFailure(view, retry)
	}
	return productui.BuildPageContent(view)
}
