package journeyclient

import "testing"

func TestTodo_UXBLIND_015(t *testing.T) {
	from := Parse("#/journeys")
	to := Parse("#/journeys/new?worker=worker-1")
	if got := ScrollModeForNavigation(from, to, false, false); got != ScrollTop {
		t.Fatalf("new route scroll mode = %v, want top", got)
	}
	if got := ScrollModeForNavigation(to, from, true, true); got != ScrollRestore {
		t.Fatalf("history route = %v, want restore", got)
	}
}

func TestTodo_UXBLIND_015_Browser(t *testing.T) {
	from := Parse("#/journeys")
	to := Parse("#/journeys/new?worker=worker-1")
	if got := ScrollModeForNavigation(from, to, false, true); got != ScrollTop {
		t.Fatalf("new route with stale saved position = %v, want top", got)
	}
	if got := ScrollModeForNavigation(to, from, true, true); got != ScrollRestore {
		t.Fatalf("history route = %v, want restore", got)
	}
}
