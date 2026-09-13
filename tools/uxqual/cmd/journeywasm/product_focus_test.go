package main

import "testing"

// TestPeopleDirectoryOnlyRouteChangeCoversEveryDirectoryFilter proves
// UXAUDIT-008's "only the data region updates on sort/filter/page-size
// changes" contract for every query parameter the People directory itself
// controls. Before this test, "eligible" (PROMOUX-001's eligible-only
// checkbox) was missing from the mutable set: toggling it looked like an
// unrelated route change, so Loading's warmRefresh branch never set
// RefreshingRegion, and the client fell back to remounting the whole page
// heading for what is, in fact, a directory-only filter change.
func TestPeopleDirectoryOnlyRouteChangeCoversEveryDirectoryFilter(t *testing.T) {
	for _, tc := range []struct {
		name, previous, current string
		want                    bool
	}{
		{"query changes", "/workspace/app/people?q=a", "/workspace/app/people?q=ab", true},
		{"team changes", "/workspace/app/people?team=a", "/workspace/app/people?team=b", true},
		{"location changes", "/workspace/app/people?location=a", "/workspace/app/people?location=b", true},
		{"eligible toggles on", "/workspace/app/people", "/workspace/app/people?eligible=1", true},
		{"eligible toggles off", "/workspace/app/people?eligible=1", "/workspace/app/people", true},
		{"sort changes", "/workspace/app/people?sort=name", "/workspace/app/people?sort=role", true},
		{"direction changes", "/workspace/app/people?dir=asc", "/workspace/app/people?dir=desc", true},
		{"page changes", "/workspace/app/people?page=1", "/workspace/app/people?page=2", true},
		{"page_size changes", "/workspace/app/people?page_size=20", "/workspace/app/people?page_size=50", true},
		{"combined filter and page change", "/workspace/app/people?eligible=1&page=1", "/workspace/app/people?eligible=1&page=2", true},
		{"no change at all", "/workspace/app/people?q=a", "/workspace/app/people?q=a", false},
		{"wrong path", "/workspace/app/home", "/workspace/app/home?q=a", false},
		{"path changes away from people", "/workspace/app/people?q=a", "/workspace/app/organization?q=a", false},
		{"an unrelated key also changes", "/workspace/app/people?q=a&nav=collapsed", "/workspace/app/people?q=b&nav=expanded", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := peopleDirectoryOnlyRouteChange(tc.previous, tc.current); got != tc.want {
				t.Errorf("peopleDirectoryOnlyRouteChange(%q, %q) = %v, want %v", tc.previous, tc.current, got, tc.want)
			}
		})
	}
}

// TestProductRouteFocusTargetKeepsFocusForDirectoryOnlyEligibleToggle pins
// the focus-handling half of the same fix: a directory-only change, eligible
// included, must not hand focus back to the page heading, because doing so
// would also defeat the point of keeping the shell mounted (the browser
// would jump the user's scroll position back to the top of the page).
func TestProductRouteFocusTargetKeepsFocusForDirectoryOnlyEligibleToggle(t *testing.T) {
	selector, caretAtEnd := productRouteFocusTarget("/workspace/app/people", "/workspace/app/people?eligible=1")
	if selector != "" || caretAtEnd {
		t.Fatalf("productRouteFocusTarget = (%q, %v), want empty selector and no caret placement", selector, caretAtEnd)
	}
}

// TestProductRouteFocusTargetFocusesHeadingOnCrossPageNavigation is the
// negative case: a real cross-page navigation must still move focus to the
// destination heading, so the directory-only carve-out is exercised, not a
// permissive default that never focuses anything.
func TestProductRouteFocusTargetFocusesHeadingOnCrossPageNavigation(t *testing.T) {
	selector, _ := productRouteFocusTarget("/workspace/app/people", "/workspace/app/organization")
	if selector != productPageFocusSelector {
		t.Fatalf("productRouteFocusTarget selector = %q, want %q", selector, productPageFocusSelector)
	}
}

func TestQueryOnlyRouteChangeRequiresTheDeclaredPath(t *testing.T) {
	if queryOnlyRouteChange("/workspace/app/home?q=a", "/workspace/app/home?q=b", peoplePath, map[string]bool{"q": true}) {
		t.Fatal("a route outside the required path must never be reported as a directory-only change")
	}
}
