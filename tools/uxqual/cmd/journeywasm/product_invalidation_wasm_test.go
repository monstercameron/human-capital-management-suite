//go:build js && wasm

package main

import (
	"context"
	"syscall/js"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// TestTodo_REV_091_03_Browser drives the browser half of the live-update
// path: a hint revalidates the resolved route quietly (the Loading render
// consumes the quiet marker exactly once), a page holding unsaved input or
// still on its first load is left alone, and a viewer without Journeys or My
// Work never subscribes.
func TestTodo_REV_091_03_Browser(t *testing.T) {
	object := js.Global().Get("Object")
	oldDocument := js.Global().Get("document")
	oldRetry, oldView, oldQuiet := productRouteRetry, lastResolvedProductView, productQuietRefreshPending
	unsaved := "false"
	var query js.Func
	query = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || args[0].String() != "[data-unsaved-protection='true']" {
			return js.Null()
		}
		root := object.New()
		dataset := object.New()
		dataset.Set("unsaved", unsaved)
		root.Set("dataset", dataset)
		return root
	})
	document := object.New()
	document.Set("querySelector", query)
	js.Global().Set("document", document)
	t.Cleanup(func() {
		js.Global().Set("document", oldDocument)
		query.Release()
		productRouteRetry, lastResolvedProductView, productQuietRefreshPending = oldRetry, oldView, oldQuiet
	})

	revalidations := 0
	productRouteRetry = func() { revalidations++ }

	// Still on the first load: nothing to refresh in place.
	lastResolvedProductView = nil
	refreshProductLiveSummaries(nil)
	if revalidations != 0 || productQuietRefreshPending {
		t.Fatal("a page with no resolved view was revalidated")
	}

	view := productui.NewView(productui.PageWork, "", "", "")
	lastResolvedProductView = &view
	refreshProductLiveSummaries(nil)
	if revalidations != 1 || !productQuietRefreshPending {
		t.Fatalf("revalidations = %d quiet = %t, want one quiet revalidation", revalidations, productQuietRefreshPending)
	}
	if !consumeProductQuietRefresh() || consumeProductQuietRefresh() {
		t.Fatal("the quiet marker must be consumed exactly once")
	}

	unsaved = "true"
	refreshProductLiveSummaries(nil)
	if revalidations != 1 || productQuietRefreshPending {
		t.Fatal("a page with unsaved input was revalidated under the reader")
	}
	if !browserHasUnsavedForm() {
		t.Fatal("unsaved input was not detected")
	}

	// A viewer without Journeys or My Work, or a service without the live
	// stream, never subscribes (and so never touches the service).
	startProductInvalidations(context.Background(), journeyclient.Config{Tenant: "acme"}, nil, func(productui.PageID) bool { return true }, nil)
	startProductInvalidations(context.Background(), journeyclient.Config{Tenant: "acme"}, noLiveService{}, func(productui.PageID) bool { return false }, nil)
}

func TestChatRefreshKeepsMountedConversationQuiet(t *testing.T) {
	oldRetry, oldQuiet := productRouteRetry, productQuietRefreshPending
	t.Cleanup(func() { productRouteRetry, productQuietRefreshPending = oldRetry, oldQuiet })
	productQuietRefreshPending = false
	refreshes := 0
	productRouteRetry = func() { refreshes++ }
	refreshChatRoute()
	if refreshes != 1 || !consumeProductQuietRefresh() || consumeProductQuietRefresh() {
		t.Fatalf("chat refreshes = %d, quiet marker must be consumed once", refreshes)
	}
}

// noLiveService is a journeyclient.Service value that offers no live
// stream; embedding the interface keeps it a Service without implementing
// any method, which is safe because startProductInvalidations must refuse it
// before calling anything.
type noLiveService struct{ journeyclient.Service }
