package journeyclient

import "testing"

func TestTodo_UXBLIND_015_SameRouteKeepsScroll(t *testing.T) {
	route := Parse("#/journeys/new?worker=worker-1")
	if got := ScrollModeForNavigation(route, route, false, true); got != ScrollKeep {
		t.Fatalf("unchanged route with a saved position = %v, want keep", got)
	}
	if got := ScrollModeForNavigation(Parse("#/journeys?worker=worker-1"), Parse("#/journeys?q=pay"), false, false); got != ScrollKeep {
		t.Fatalf("list filter route = %v, want keep", got)
	}
	if got := ScrollModeForNavigation(route, Parse("#/journeys/new?worker=worker-2"), false, false); got != ScrollTop {
		t.Fatalf("new proposal subject = %v, want top", got)
	}
	if got := ScrollModeForNavigation(route, Parse("#/journeys"), false, false); got != ScrollTop {
		t.Fatalf("new route = %v, want top", got)
	}
}
