package main

import (
	"fmt"
	"testing"
	"time"
)

// UXLIVE-028: the history router owns one main-content scroll policy keyed
// by history entry and canonical resource identity. These tests drive the
// DOM-free ledger the js/wasm adapter feeds.

type uxlive028Clock struct{ now time.Time }

func (clock *uxlive028Clock) Now() time.Time { return clock.now }

func uxlive028Ledger() (*productScrollLedger, *uxlive028Clock) {
	clock := &uxlive028Clock{now: time.Date(2026, 9, 19, 9, 0, 0, 0, time.UTC)}
	return newProductScrollLedger(clock.Now), clock
}

// settle mimics the router: the entry index is what the product history
// controller stamps into history.state.
func uxlive028Settle(ledger *productScrollLedger, index int, route string) (productScrollAction, float64) {
	return ledger.Settle(productScrollEntryKey("ledger", index, route), route)
}

// TestTodo_UXLIVE_028 is the GREEN contract: forward navigation to another
// page or resource starts at the top, Back/Forward restore the entry's own
// position, and query-only changes keep the reader where they were.
func TestTodo_UXLIVE_028(t *testing.T) {
	ledger, _ := uxlive028Ledger()
	if action, _ := uxlive028Settle(ledger, 0, "/workspace/app/people?"); action != productScrollKeep {
		t.Fatalf("cold document action = %v, want keep", action)
	}
	ledger.Observe(900)

	// Forward to a different page.
	ledger.BeginNavigation(false)
	if action, top := uxlive028Settle(ledger, 1, "/workspace/app/organization?"); action != productScrollTop || top != 0 {
		t.Fatalf("People -> Organization = %v %v, want top 0", action, top)
	}
	ledger.Observe(240)

	// A query-only change of the same resource keeps its position.
	ledger.BeginNavigation(false)
	if action, top := uxlive028Settle(ledger, 2, "/workspace/app/organization?q=Jane"); action != productScrollKeep || top != 240 {
		t.Fatalf("organization filter = %v %v, want keep 240", action, top)
	}
	ledger.Observe(260)

	// Forward to a different resource on the same page family.
	ledger.BeginNavigation(false)
	if action, _ := uxlive028Settle(ledger, 3, "/workspace/app/person?person=w-1"); action != productScrollTop {
		t.Fatalf("open person = %v, want top", action)
	}
	ledger.Observe(75)
	ledger.BeginNavigation(false)
	if action, _ := uxlive028Settle(ledger, 4, "/workspace/app/person?person=w-2"); action != productScrollTop {
		t.Fatalf("different person = %v, want top", action)
	}

	// Back to the first person restores that entry's position.
	ledger.BeginNavigation(true)
	if action, top := uxlive028Settle(ledger, 3, "/workspace/app/person?person=w-1"); action != productScrollRestore || top != 75 {
		t.Fatalf("Back to person w-1 = %v %v, want restore 75", action, top)
	}
	// Back to the filtered organization entry restores its own position.
	ledger.BeginNavigation(true)
	if action, top := uxlive028Settle(ledger, 2, "/workspace/app/organization?q=Jane"); action != productScrollRestore || top != 260 {
		t.Fatalf("Back to filtered organization = %v %v, want restore 260", action, top)
	}
	ledger.BeginNavigation(true)
	if action, _ := uxlive028Settle(ledger, 1, "/workspace/app/organization?"); action != productScrollKeep {
		t.Fatalf("Back across a filter change = %v, want keep", action)
	}
	ledger.BeginNavigation(true)
	if action, top := uxlive028Settle(ledger, 0, "/workspace/app/people?"); action != productScrollRestore || top != 900 {
		t.Fatalf("Back to People = %v %v, want restore 900", action, top)
	}
	// Forward returns to Organization's own entry at the position the reader
	// left it (the kept 260 from the filter change), not People's 900.
	ledger.BeginNavigation(true)
	if action, top := uxlive028Settle(ledger, 1, "/workspace/app/organization?"); action != productScrollRestore || top != 260 {
		t.Fatalf("Forward to Organization = %v %v, want restore 260", action, top)
	}
}

// TestTodo_UXLIVE_028_Regression pins the RED: the previous page's reading
// offset must not leak into the destination, and the browser clamping the
// old page against shorter loading content must not overwrite what the
// reader left.
func TestTodo_UXLIVE_028_Regression(t *testing.T) {
	ledger, _ := uxlive028Ledger()
	uxlive028Settle(ledger, 0, "/workspace/app/people?")
	ledger.Observe(1200)
	ledger.BeginNavigation(false)
	// The loading proxy is shorter: the browser clamps and fires scroll.
	ledger.Observe(310)
	ledger.Observe(0)
	for _, route := range []string{"/workspace/app/organization?", "/workspace/app/insights?", "/workspace/app/myself?"} {
		action, top := uxlive028Settle(ledger, 1, route)
		if action != productScrollTop || top != 0 {
			t.Fatalf("%s inherited %v %v, want top 0", route, action, top)
		}
		ledger.BeginNavigation(false)
	}
	if saved, ok := ledger.Saved(productScrollEntryKey("ledger", 0, "/workspace/app/people?")); !ok || saved != 1200 {
		t.Fatalf("People's saved position = %v %v, want 1200 (clamp events leaked into the ledger)", saved, ok)
	}

	// A navigation that never renders must not freeze recording forever.
	stuck, stuckClock := uxlive028Ledger()
	uxlive028Settle(stuck, 0, "/workspace/app/people?")
	stuck.BeginNavigation(false)
	stuck.Observe(50)
	if saved, _ := stuck.Saved(productScrollEntryKey("ledger", 0, "/workspace/app/people?")); saved != 0 {
		t.Fatalf("recorded %v while a navigation was settling", saved)
	}
	stuckClock.now = stuckClock.now.Add(productScrollPendingLimit + time.Millisecond)
	stuck.Observe(80)
	if saved, _ := stuck.Saved(productScrollEntryKey("ledger", 0, "/workspace/app/people?")); saved != 80 {
		t.Fatalf("recording stayed frozen after the pending limit: %v", saved)
	}

	// The ledger is bounded.
	bounded, _ := uxlive028Ledger()
	for index := 0; index < productScrollLedgerCapacity+50; index++ {
		bounded.BeginNavigation(false)
		uxlive028Settle(bounded, index, fmt.Sprintf("/workspace/app/person?person=w-%d", index))
	}
	if len(bounded.positions) > productScrollLedgerCapacity || len(bounded.order) != len(bounded.positions) {
		t.Fatalf("ledger grew to %d positions (%d ordered), cap %d", len(bounded.positions), len(bounded.order), productScrollLedgerCapacity)
	}

	// Keys separate entries of the same resource and resources of one entry.
	if productScrollEntryKey("ledger", 1, "/workspace/app/people?q=a") != productScrollEntryKey("ledger", 1, "/workspace/app/people?sort=name") {
		t.Fatal("presentation state split one resource's identity")
	}
	if productScrollEntryKey("ledger", 1, "/workspace/app/person?person=a") == productScrollEntryKey("ledger", 1, "/workspace/app/person?person=b") {
		t.Fatal("two people shared one scroll identity")
	}
	if productScrollEntryKey("ledger", 1, "/workspace/app/people") == productScrollEntryKey("ledger", 2, "/workspace/app/people") {
		t.Fatal("two history entries shared one saved position")
	}
	var nilLedger *productScrollLedger
	nilLedger.Observe(1)
	nilLedger.BeginNavigation(true)
	if action, _ := nilLedger.Settle("k", "/workspace/app/home"); action != productScrollKeep {
		t.Fatal("nil ledger moved the region")
	}
	if _, ok := nilLedger.Saved("k"); ok {
		t.Fatal("nil ledger reported a saved position")
	}
	for action, want := range map[productScrollAction]string{productScrollKeep: "keep", productScrollTop: "top", productScrollRestore: "restore", productScrollAction(9): "unknown"} {
		if got := action.String(); got != want {
			t.Fatalf("%d.String() = %q, want %q", int(action), got, want)
		}
	}
}

// TestTodo_UXLIVE_028_Accessibility: whenever the policy starts or restores
// a different resource, focus (and with it the route announcement) moves to
// the new page identity; a kept position keeps the initiating control's focus.
func TestTodo_UXLIVE_028_Accessibility(t *testing.T) {
	cases := []struct {
		previous, current string
		traversal         bool
	}{
		{"/workspace/app/people?", "/workspace/app/organization?", false},
		{"/workspace/app/organization?", "/workspace/app/people?", true},
		{"/workspace/app/person?person=a", "/workspace/app/person?person=b", false},
		{"/workspace/app/people?page=1", "/workspace/app/people?page=2", false},
		{"/workspace/app/people?sort=name", "/workspace/app/people?sort=name&dir=desc", true},
		{"/workspace/app/organization?", "/workspace/app/organization?q=Jane", false},
	}
	for _, tc := range cases {
		action, _ := decideProductScroll(tc.previous, tc.current, tc.traversal, 120, true)
		selector, _ := productRouteFocusTarget(tc.previous, tc.current)
		movesToIdentity := action != productScrollKeep
		if movesToIdentity && selector != productPageFocusSelector {
			t.Fatalf("%s -> %s moved the viewport (%v) without focusing the page identity (%q)", tc.previous, tc.current, action, selector)
		}
		if !movesToIdentity && selector != "" {
			t.Fatalf("%s -> %s kept the position but stole focus to %q", tc.previous, tc.current, selector)
		}
	}
}
