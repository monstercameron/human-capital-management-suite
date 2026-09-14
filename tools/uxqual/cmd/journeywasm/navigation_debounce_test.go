package main

import (
	"reflect"
	"testing"
	"time"
)

type fakeDebounceTimer struct{ stopped bool }

func (t *fakeDebounceTimer) Stop() bool {
	wasActive := !t.stopped
	t.stopped = true
	return wasActive
}

type scheduledDebounce struct {
	delay    time.Duration
	callback func()
	timer    *fakeDebounceTimer
}

func TestNavigationDebouncerUses250MillisecondsAndRejectsStaleCallbacks(t *testing.T) {
	var calls []string
	var scheduled []*scheduledDebounce
	debouncer := newNavigationDebouncerWithScheduler(func(href string) {
		calls = append(calls, href)
	}, func(delay time.Duration, callback func()) debounceTimer {
		entry := &scheduledDebounce{delay: delay, callback: callback, timer: &fakeDebounceTimer{}}
		scheduled = append(scheduled, entry)
		return entry.timer
	})

	debouncer.Schedule("/workspace/app/home?menu_q=w")
	debouncer.Schedule("/workspace/app/home?menu_q=work")
	if len(scheduled) != 2 || scheduled[0].delay != 250*time.Millisecond || scheduled[1].delay != 250*time.Millisecond {
		t.Fatalf("scheduled debounce = %+v", scheduled)
	}
	if !scheduled[0].timer.stopped {
		t.Fatal("second input did not stop the first timer")
	}
	scheduled[0].callback()
	if len(calls) != 0 {
		t.Fatalf("stale callback navigated: %v", calls)
	}
	scheduled[1].callback()
	if !reflect.DeepEqual(calls, []string{"/workspace/app/home?menu_q=work"}) {
		t.Fatalf("navigation calls = %v", calls)
	}

	debouncer.Schedule("/workspace/app/home?menu_q=people")
	latest := scheduled[len(scheduled)-1]
	debouncer.Cancel()
	latest.callback()
	if !latest.timer.stopped || len(calls) != 1 {
		t.Fatalf("cancelled timer state=%+v calls=%v", latest.timer, calls)
	}
}

func TestProductRouteFocusTargetKeepsMenuInputForQueryOnlyChanges(t *testing.T) {
	selector, caret := productRouteFocusTarget(
		"/workspace/app/home?favorites=people",
		"/workspace/app/home?favorites=people&menu_q=work",
	)
	if selector != menuFilterFocusSelector || !caret {
		t.Fatalf("menu-filter target = %q caret=%t", selector, caret)
	}

	for _, pair := range [][2]string{
		{"/workspace/app/home?menu_q=work", "/workspace/app/people?menu_q=work"},
	} {
		selector, caret = productRouteFocusTarget(pair[0], pair[1])
		if selector != productPageFocusSelector || caret {
			t.Fatalf("ordinary route %q -> %q target=%q caret=%t", pair[0], pair[1], selector, caret)
		}
	}
}

func TestNavigationOnlyRouteChangePreservesShellFocusAndDetectsCollapse(t *testing.T) {
	previous := "/workspace/app/admin?locale=en-US"
	collapsed := "/workspace/app/admin?locale=en-US&nav=collapsed"
	selector, caret := productRouteFocusTarget(previous, collapsed)
	if selector != "" || caret {
		t.Fatalf("navigation-only focus = (%q, %v), want no forced focus", selector, caret)
	}
	if !navigationCollapsedRouteChange(previous, collapsed) {
		t.Fatal("expected collapsed navigation transition to be detected")
	}
	if navigationCollapsedRouteChange(collapsed, "/workspace/app/admin?locale=en-US&nav=expanded") {
		t.Fatal("expanded transition must not request a collapsed-scroll reset")
	}
}

func TestProductRouteFocusTargetKeepsPeopleCollectionViewportStable(t *testing.T) {
	for _, pair := range [][2]string{
		{"/workspace/app/people?locale=en-US&page_size=100", "/workspace/app/people?locale=en-US&page_size=100&sort=role"},
		{"/workspace/app/people?locale=en-US&page_size=100&sort=role", "/workspace/app/people?dir=desc&locale=en-US&page_size=100&sort=role"},
		{"/workspace/app/people?locale=en-US&page=2", "/workspace/app/people?locale=en-US&page=3"},
		{"/workspace/app/people?locale=en-US", "/workspace/app/people?locale=en-US&team=Finance"},
	} {
		selector, caret := productRouteFocusTarget(pair[0], pair[1])
		if selector != "" || caret {
			t.Fatalf("people collection route %q -> %q target=%q caret=%t, want no focus move", pair[0], pair[1], selector, caret)
		}
	}

	for _, pair := range [][2]string{
		{"/workspace/app/people?locale=en-US", "/workspace/app/person?locale=en-US&person=worker-1"},
		{"/workspace/app/person?person=worker-1", "/workspace/app/person?person=worker-2"},
		{"/workspace/app/journeys?mode=new&worker=worker-1", "/workspace/app/journeys?journey=journey-1"},
	} {
		selector, caret := productRouteFocusTarget(pair[0], pair[1])
		if selector != productPageFocusSelector || caret {
			t.Fatalf("page route %q -> %q target=%q caret=%t", pair[0], pair[1], selector, caret)
		}
		if !productRouteShouldResetMainScroll(pair[0], pair[1]) {
			t.Fatalf("destination route %q -> %q did not reset main scroll", pair[0], pair[1])
		}
	}

	for _, pair := range [][2]string{
		{"/workspace/app/people?locale=en-US", "/workspace/app/people?locale=fr-FR"},
		{"/workspace/app/people?locale=en-US", "/workspace/app/people?favorites=people&locale=en-US"},
		{"/workspace/app/history?history_sort=closed", "/workspace/app/history?history_dir=desc&history_sort=closed"},
		{"/workspace/app/person?person=worker-1", "/workspace/app/person?person=worker-1&workflow_q=promotion"},
	} {
		selector, caret := productRouteFocusTarget(pair[0], pair[1])
		if selector != "" || caret {
			t.Fatalf("presentation route %q -> %q target=%q caret=%t, want no focus move", pair[0], pair[1], selector, caret)
		}
		if productRouteShouldResetMainScroll(pair[0], pair[1]) {
			t.Fatalf("presentation route %q -> %q unexpectedly reset main scroll", pair[0], pair[1])
		}
	}
}

func TestTodo_UXAUDIT_008_Regression_PeopleRegionRoutes(t *testing.T) {
	base := "/workspace/app/people?locale=en-US&page_size=100"
	for _, query := range []string{
		"&q=Jane", "&team=Finance", "&location=Boston", "&eligible=1",
		"&sort=role", "&dir=desc", "&page=2",
	} {
		if !peopleDirectoryOnlyRouteChange(base, base+query) {
			t.Errorf("People collection change %q refreshed more than the directory", query)
		}
	}
	if !peopleDirectoryOnlyRouteChange(base, "/workspace/app/people?locale=en-US&page_size=20") {
		t.Fatal("changing the selected page size refreshed more than the directory")
	}
	for _, changed := range []string{
		"/workspace/app/person?locale=en-US&page_size=100",
		base + "&nav=collapsed",
		base + "&locale=de-DE",
	} {
		if peopleDirectoryOnlyRouteChange(base, changed) {
			t.Errorf("non-directory route %q was mistaken for a People-only change", changed)
		}
	}
}

func TestProductRouteDestinationIdentityIsExplicitAndBounded(t *testing.T) {
	for _, tc := range []struct {
		name              string
		previous, current string
		wantReset         bool
	}{
		{name: "new page", previous: "/workspace/app/people", current: "/workspace/app/history", wantReset: true},
		{name: "new person", previous: "/workspace/app/person?person=a", current: "/workspace/app/person?person=b", wantReset: true},
		{name: "journey list to proposal", previous: "/workspace/app/journeys", current: "/workspace/app/journeys?mode=new&worker=a", wantReset: true},
		{name: "proposal to created journey", previous: "/workspace/app/journeys?mode=new&worker=a", current: "/workspace/app/journeys?journey=j-1", wantReset: true},
		{name: "journey presentation state", previous: "/workspace/app/journeys?journey=j-1", current: "/workspace/app/journeys?favorites=journeys&journey=j-1", wantReset: false},
		{name: "directory sorting", previous: "/workspace/app/people?page=2", current: "/workspace/app/people?page=2&sort=role", wantReset: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := productRouteShouldResetMainScroll(tc.previous, tc.current); got != tc.wantReset {
				t.Fatalf("reset main scroll = %v, want %v", got, tc.wantReset)
			}
		})
	}
}
