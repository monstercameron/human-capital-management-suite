//go:build js && wasm

package main

import (
	"context"
	"syscall/js"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// productQuietRefreshPending marks the next route revalidation as a live
// update rather than a reader action: the Loading render keeps the last
// authorized page exactly as it is (no progress bar, no busy announcement)
// while the loader re-reads, so a live update never flickers the page.
var productQuietRefreshPending bool

// consumeProductQuietRefresh reports and clears the quiet marker. Loading
// consumes it on every render, so a later reader-initiated refresh of the
// same page is never mistaken for a live update.
func consumeProductQuietRefresh() bool {
	quiet := productQuietRefreshPending
	productQuietRefreshPending = false
	return quiet
}

// startProductInvalidations subscribes the product shell to live promotion
// updates (REV-091-03) for the viewer's lifetime on the page. It does
// nothing for a viewer who may see neither Journeys nor My Work, or when the
// service offers no live stream.
func startProductInvalidations(ctx context.Context, cfg journeyclient.Config, service journeyclient.Service, canView func(productui.PageID) bool, journeyApp *journeyclient.App) {
	live, ok := service.(journeyclient.InvalidationService)
	if !ok || canView == nil || !(canView(productui.PageJourneys) || canView(productui.PageWork)) {
		return
	}
	scope, err := productInvalidationScope(cfg.Tenant)
	if err != nil {
		return
	}
	coalescer := newRefreshCoalescer(browserDebounceScheduler, productInvalidationDebounce, func() error {
		refreshProductLiveSummaries(journeyApp)
		return nil
	})
	runner := newProductInvalidationRunner(scope, live, coalescer)
	go func() { _ = runner.run(ctx) }()
}

// refreshProductLiveSummaries re-reads what a promotion transition can
// change: the resolved route's datasets (shell counts, Home, My Work, Person)
// through a quiet in-place revalidation, and the Journeys list through the
// journey client's own quiet list read. The route stays where it is, so
// focus and the main region's scroll position are untouched. A page with
// unsaved form input is left alone; the next hint or navigation catches up.
func refreshProductLiveSummaries(journeyApp *journeyclient.App) {
	if lastResolvedProductView == nil || browserHasUnsavedForm() {
		return
	}
	if productRouteRetry != nil {
		productQuietRefreshPending = true
		productRouteRetry()
	}
	if journeyApp != nil {
		journeyApp.RefreshListQuietly()
	}
}

func browserHasUnsavedForm() bool {
	document := js.Global().Get("document")
	if !document.Truthy() {
		return false
	}
	root := document.Call("querySelector", "[data-unsaved-protection='true']")
	return root.Truthy() && root.Get("dataset").Get("unsaved").String() == "true"
}
