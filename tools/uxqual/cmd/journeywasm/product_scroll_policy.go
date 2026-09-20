package main

import (
	"net/url"
	"strconv"
	"sync"
	"time"
)

// productScrollAction is the one decision the history router makes about the
// main content owner's scroll position after a software-routed change
// (UXLIVE-028). Pages never scroll the main region themselves; they render,
// and this policy decides.
type productScrollAction int

const (
	// productScrollKeep leaves the reader where they were: a sort, a page of
	// results, a filter, an overlay or any other query-only change of the same
	// resource.
	productScrollKeep productScrollAction = iota
	// productScrollTop starts a newly opened page or resource at its identity.
	productScrollTop
	// productScrollRestore returns Back/Forward to the position the reader
	// left that history entry at.
	productScrollRestore
)

func (action productScrollAction) String() string {
	switch action {
	case productScrollKeep:
		return "keep"
	case productScrollTop:
		return "top"
	case productScrollRestore:
		return "restore"
	default:
		return "unknown"
	}
}

// productScrollPendingLimit bounds how long a navigation may suppress scroll
// recording. A navigation that never renders (a push to the address already
// shown) must not freeze the ledger for the rest of the session.
const productScrollPendingLimit = 2 * time.Second

// productScrollLedgerCapacity bounds the in-memory position ledger. Entries
// are small, but a long session must not grow it without limit.
const productScrollLedgerCapacity = 200

// productRouteResourceIdentity is the canonical resource an address names:
// its page path plus the query keys that select a record or workflow on that
// page. Presentation state (sort, paging, filters, overlays, locale, menu
// state) is deliberately excluded, so two addresses that differ only in it
// share one identity.
func productRouteResourceIdentity(route string) string {
	parsed, err := url.Parse(route)
	if err != nil {
		return route
	}
	identity := parsed.Path
	query := parsed.Query()
	for _, key := range productResourceRouteKeys(parsed.Path) {
		identity += "|" + key + "=" + query.Get(key)
	}
	return identity
}

// productScrollEntryKey keys a saved position by the history entry (the
// ledger id and index the product history controller stamps into
// history.state) and the canonical resource shown in it. An entry without a
// ledger stamp falls back to its resource identity alone.
func productScrollEntryKey(ledgerID string, index int, route string) string {
	identity := productRouteResourceIdentity(route)
	if ledgerID == "" {
		return identity
	}
	return ledgerID + "#" + strconv.Itoa(index) + "|" + identity
}

// decideProductScroll is the router's scroll policy. A change that keeps the
// same resource keeps its position; a traversal (Back/Forward) to a
// different resource restores what that entry saved; every other change of
// resource starts at the top.
func decideProductScroll(previous, current string, traversal bool, saved float64, hasSaved bool) (productScrollAction, float64) {
	if previous != "" && !productRouteDestinationChanged(previous, current) {
		return productScrollKeep, 0
	}
	if traversal && hasSaved {
		return productScrollRestore, saved
	}
	return productScrollTop, 0
}

// productScrollLedger is the history router's single owner of main-content
// scroll positions. It is DOM-free so the policy can be exercised natively;
// the js/wasm adapter feeds it the browser's events.
type productScrollLedger struct {
	mu        sync.Mutex
	positions map[string]float64
	order     []string
	// activeKey is the entry whose content the main region is showing.
	activeKey string
	// activeRoute is that entry's address.
	activeRoute string
	// lastTop is the most recent position observed for activeKey, kept so a
	// navigation can save the position the reader actually left even if the
	// browser has already clamped the region for the next render.
	lastTop float64
	// pending suppresses recording between the start of a navigation and
	// the render that settles it: scroll events in between are the browser
	// clamping an old page against new content, not the reader scrolling.
	pending      bool
	pendingSince time.Time
	traversal    bool
	now          func() time.Time
}

func newProductScrollLedger(now func() time.Time) *productScrollLedger {
	if now == nil {
		now = time.Now
	}
	return &productScrollLedger{positions: make(map[string]float64), now: now}
}

// Observe records a reader's scroll of the main region for the entry it is
// showing. It is ignored while a navigation is settling.
func (ledger *productScrollLedger) Observe(top float64) {
	if ledger == nil {
		return
	}
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if ledger.pending && ledger.now().Sub(ledger.pendingSince) < productScrollPendingLimit {
		return
	}
	ledger.pending = false
	ledger.lastTop = top
	if ledger.activeKey != "" {
		ledger.save(ledger.activeKey, top)
	}
}

// BeginNavigation saves the departing entry's position and suppresses
// recording until Settle. traversal marks Back/Forward.
func (ledger *productScrollLedger) BeginNavigation(traversal bool) {
	if ledger == nil {
		return
	}
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if ledger.activeKey != "" && !ledger.pending {
		ledger.save(ledger.activeKey, ledger.lastTop)
	}
	ledger.pending = true
	ledger.pendingSince = ledger.now()
	ledger.traversal = ledger.traversal || traversal
}

// Settle adopts the entry now rendered and returns what the router must do
// with the main region. For a kept position the returned top is the
// position the reader left the previous entry at, so a re-mounted region can
// be put back; the caller applies it only when the region actually moved.
func (ledger *productScrollLedger) Settle(key, route string) (productScrollAction, float64) {
	if ledger == nil {
		return productScrollKeep, 0
	}
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	previousRoute := ledger.activeRoute
	saved, hasSaved := ledger.positions[key]
	action, top := decideProductScroll(previousRoute, route, ledger.traversal, saved, hasSaved)
	if previousRoute == "" {
		// The cold document keeps the browser's natural position.
		action, top = productScrollKeep, ledger.lastTop
	}
	if action == productScrollKeep {
		top = ledger.lastTop
	}
	ledger.activeKey, ledger.activeRoute = key, route
	ledger.pending, ledger.traversal = false, false
	ledger.lastTop = top
	ledger.save(key, top)
	return action, top
}

// Saved reports the recorded position for key.
func (ledger *productScrollLedger) Saved(key string) (float64, bool) {
	if ledger == nil {
		return 0, false
	}
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	top, ok := ledger.positions[key]
	return top, ok
}

func (ledger *productScrollLedger) save(key string, top float64) {
	if _, exists := ledger.positions[key]; !exists {
		ledger.order = append(ledger.order, key)
		if len(ledger.order) > productScrollLedgerCapacity {
			oldest := ledger.order[0]
			ledger.order = ledger.order[1:]
			delete(ledger.positions, oldest)
		}
	}
	ledger.positions[key] = top
}
