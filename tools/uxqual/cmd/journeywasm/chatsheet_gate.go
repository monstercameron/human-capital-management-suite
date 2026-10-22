package main

import (
	"net/url"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// Chat's rules are a stylesheet of their own that a document links only on a
// Chat page (internal/humanwork/productui/chatsheet_split.go). A person who
// reaches Chat from another page never loaded a document that links it, so the
// client loads it on the way in, and must not paint Chat before it has: the
// Chat frame without its rules is a column of unstyled text for as long as the
// sheet takes, which is the flash this gate exists to prevent.
//
// The gate holds a navigation to a Chat page until the sheet has loaded. It is
// a plain state machine so the order of events can be tested without a
// browser; chatsheet_wasm.go gives it the page's elements and timer.

type chatSheetState int

const (
	// chatSheetUnnamed: the document neither links the sheet nor says where it
	// is (a document that is not a workspace shell). There is nothing to wait
	// for; Chat shows as the document's rules style it.
	chatSheetUnnamed chatSheetState = iota
	// chatSheetNamed: the document names the sheet and has not loaded it.
	chatSheetNamed
	// chatSheetLoading: the sheet was asked for and has not answered.
	chatSheetLoading
	// chatSheetReady: the sheet is in the document, from the first paint or
	// because it loaded.
	chatSheetReady
	// chatSheetFailed: the sheet did not load. Navigation proceeds anyway:
	// Chat works without it, and the person must not be stuck on the page they
	// are leaving.
	chatSheetFailed
)

// chatSheetGate holds navigations into Chat until Chat's sheet is in the page.
// It is safe for use from the page's one thread and from route loaders, which
// run on goroutines of their own.
type chatSheetGate struct {
	mu      sync.Mutex
	state   chatSheetState
	pending []func()
	// load asks the page for the sheet and calls done when the browser reports
	// that it loaded (true) or failed to (false). It is called at most once.
	load func(done func(loaded bool))
	// settled is closed when the gate leaves the named and loading states.
	settled chan struct{}
}

func newChatSheetGate(state chatSheetState, load func(done func(loaded bool))) *chatSheetGate {
	g := &chatSheetGate{state: state, load: load, settled: make(chan struct{})}
	if state != chatSheetNamed && state != chatSheetLoading {
		close(g.settled)
	}
	return g
}

// chatSheetPageForHref is the product page an address opens, or false when it
// opens none (a link away from the product, a malformed address).
func chatSheetPageForHref(href string) (productui.PageID, bool) {
	parsed, err := url.Parse(href)
	if err != nil {
		return "", false
	}
	definition, ok := productui.LookupRoute(parsed.Path)
	if !ok {
		return "", false
	}
	return definition.ID, true
}

// Holds reports whether Navigate to href would wait for the sheet. A caller that
// can only decide before it acts (the history listener, which has to stop an
// event before the router sees it) asks first and then calls Navigate.
func (g *chatSheetGate) Holds(href string) bool {
	page, ok := chatSheetPageForHref(href)
	if !ok || !productui.PageNeedsChatStylesheet(page) {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.state == chatSheetNamed || g.state == chatSheetLoading
}

// Arrive is the gate for an arrival by Back or Forward: the browser has already
// moved the address, and the router will load the page for it as soon as it sees
// the event. It reports true when the arrival is Chat's and the sheet is not in
// the page yet; the caller then keeps the event from the router until release
// runs, which is when the sheet has loaded or given up. It reports false when
// the router may have the event now.
func (g *chatSheetGate) Arrive(href string, release func()) bool {
	if !g.Holds(href) {
		return false
	}
	g.Navigate(href, release)
	return true
}

// Navigate runs proceed, now when the address opens a page that does not need
// Chat's sheet or the sheet is already in the page, and otherwise once it has
// loaded. proceed runs exactly once, on the thread that finishes the load or on
// the caller's when nothing needs waiting for.
func (g *chatSheetGate) Navigate(href string, proceed func()) {
	page, ok := chatSheetPageForHref(href)
	if !ok || !productui.PageNeedsChatStylesheet(page) {
		proceed()
		return
	}
	g.mu.Lock()
	switch g.state {
	case chatSheetUnnamed, chatSheetReady, chatSheetFailed:
		g.mu.Unlock()
		proceed()
		return
	}
	g.pending = append(g.pending, proceed)
	start := g.startLocked()
	g.mu.Unlock()
	start()
}

// Wait blocks until Chat's sheet is in the page or has failed to load, or until
// cancelled. A route loader calls it before it returns a Chat page, so a way
// into Chat that did not come through Navigate (history, a link the router
// handled) still never resolves Chat before its rules are there.
func (g *chatSheetGate) Wait(cancelled <-chan struct{}) {
	g.mu.Lock()
	if g.state == chatSheetNamed {
		start := g.startLocked()
		g.mu.Unlock()
		start()
	} else {
		g.mu.Unlock()
	}
	select {
	case <-g.settled:
	case <-cancelled:
	}
}

// startLocked moves a named gate to loading and returns the call that asks the
// page for the sheet; the caller makes it after releasing the lock. For a gate
// that is already loading it returns a call that does nothing.
func (g *chatSheetGate) startLocked() func() {
	if g.state != chatSheetNamed {
		return func() {}
	}
	g.state = chatSheetLoading
	load := g.load
	if load == nil {
		return func() { g.finish(false) }
	}
	return func() { load(g.finish) }
}

// finish records the outcome and releases everything that was waiting, in the
// order it arrived. A second report (a load event after the timer gave up) is
// ignored.
func (g *chatSheetGate) finish(loaded bool) {
	g.mu.Lock()
	if g.state != chatSheetLoading {
		g.mu.Unlock()
		return
	}
	if loaded {
		g.state = chatSheetReady
	} else {
		g.state = chatSheetFailed
	}
	waiting := g.pending
	g.pending = nil
	close(g.settled)
	g.mu.Unlock()
	for _, proceed := range waiting {
		proceed()
	}
}

// State reports where the gate is, for tests.
func (g *chatSheetGate) State() chatSheetState {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.state
}
