package main

import (
	"net/url"
	"strings"
)

// docsPath is the Docs page: the library list, or one document when the
// address carries ?document=.
const docsPath = "/workspace/app/docs"

// docsListHeadingSelector is the Docs list's heading (productui's
// docsLibraryHeader).
const docsListHeadingSelector = "#docs-list-heading"

// docsListAddressCapacity bounds how many list addresses keep a position.
const docsListAddressCapacity = 50

// docsListRoute reports a Docs library list address (no open document).
func docsListRoute(route string) bool {
	parsed, err := url.Parse(route)
	return err == nil && parsed.Path == docsPath && strings.TrimSpace(parsed.Query().Get("document")) == ""
}

// docsDocumentRoute reports a Docs address that opens one document.
func docsDocumentRoute(route string) bool {
	parsed, err := url.Parse(route)
	return err == nil && parsed.Path == docsPath && strings.TrimSpace(parsed.Query().Get("document")) != ""
}

// docsListAddressKey is a list address with its query in canonical order and
// shell-only state (the navigation rail) left out, so "Back to documents"
// and the entry the reader left name the same list.
func docsListAddressKey(route string) string {
	parsed, err := url.Parse(route)
	if err != nil {
		return route
	}
	query := parsed.Query()
	query.Del("nav")
	return parsed.Path + "?" + query.Encode()
}

// docsPagerOnlyRouteChange reports a change of only the list's page or page
// size. The reader asked for other rows, so the list starts at its top
// instead of leaving them at the bottom of the new page.
func docsPagerOnlyRouteChange(previous, current string) bool {
	return queryOnlyRouteChange(previous, current, docsPath, map[string]bool{"docs_page": true, "docs_size": true})
}

// docsListMemory keeps the last position of each Docs list address, so a
// return from a document -- "Back to documents" (a new history entry), the
// header arrows or the browser's Back -- lands where the reader left the
// list, even when the history entry itself has no saved position.
type docsListMemory struct {
	positions map[string]float64
	order     []string
}

func (memory *docsListMemory) save(route string, top float64) {
	if !docsListRoute(route) {
		return
	}
	if memory.positions == nil {
		memory.positions = map[string]float64{}
	}
	key := docsListAddressKey(route)
	if _, exists := memory.positions[key]; !exists {
		memory.order = append(memory.order, key)
		if len(memory.order) > docsListAddressCapacity {
			delete(memory.positions, memory.order[0])
			memory.order = memory.order[1:]
		}
	}
	memory.positions[key] = top
}

func (memory *docsListMemory) lookup(route string) (float64, bool) {
	top, ok := memory.positions[docsListAddressKey(route)]
	return top, ok && top > 0
}

// decideDocsScroll refines the router's decision for the Docs page. A pager
// change starts the list at the top (Back/Forward still restore the entry's
// own position), and a return from a document to a list restores where the
// reader left that list.
func decideDocsScroll(previous, current string, traversal bool, action productScrollAction, top float64, saved float64, hasSaved bool, memory *docsListMemory) (productScrollAction, float64) {
	if docsPagerOnlyRouteChange(previous, current) {
		if traversal && hasSaved {
			return productScrollRestore, saved
		}
		return productScrollTop, 0
	}
	if action == productScrollTop && docsDocumentRoute(previous) && docsListRoute(current) && memory != nil {
		if remembered, ok := memory.lookup(current); ok {
			return productScrollRestore, remembered
		}
	}
	return action, top
}
