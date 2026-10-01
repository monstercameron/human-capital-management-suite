package productui

import "testing"

func TestTodo_UXBLIND_098_LiveSearch(t *testing.T) {
	tests := []struct {
		name           string
		routeChanged   bool
		routeQuery     string
		localQuery     string
		settledQuery   string
		wantRouteQuery string
		wantRouteSync  bool
		wantNavigation bool
	}{
		{
			name:           "settled input navigates while route props are stale",
			routeChanged:   false,
			routeQuery:     "",
			localQuery:     "Curtis",
			settledQuery:   "Curtis",
			wantRouteQuery: "Curtis",
			wantNavigation: true,
		},
		{
			name:           "route update wins over an older pending debounce",
			routeChanged:   true,
			routeQuery:     "Ana",
			localQuery:     "Curtis",
			settledQuery:   "",
			wantRouteQuery: "Ana",
			wantRouteSync:  true,
		},
		{
			name:           "stale debounce cannot navigate after local input changed",
			routeChanged:   false,
			routeQuery:     "",
			localQuery:     "Curtis",
			settledQuery:   "Cur",
			wantRouteQuery: "Curtis",
		},
		{
			name:           "settled route query does not navigate again",
			routeChanged:   false,
			routeQuery:     "Curtis",
			localQuery:     "Curtis",
			settledQuery:   "Curtis",
			wantRouteQuery: "Curtis",
		},
		{
			name:           "route synchronization ignores surrounding whitespace",
			routeChanged:   true,
			routeQuery:     " Curtis ",
			localQuery:     "Curtis",
			settledQuery:   "Curtis",
			wantRouteQuery: "Curtis",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotRouteQuery, gotRouteSync := peopleFilterRouteQuery(test.routeChanged, test.routeQuery, test.localQuery)
			if gotRouteQuery != test.wantRouteQuery || gotRouteSync != test.wantRouteSync {
				t.Fatalf("route sync = (%q, %t), want (%q, %t)", gotRouteQuery, gotRouteSync, test.wantRouteQuery, test.wantRouteSync)
			}
			if got := peopleFilterQueryShouldNavigate(test.routeChanged, test.routeQuery, test.localQuery, test.settledQuery); got != test.wantNavigation {
				t.Fatalf("should navigate = %t, want %t", got, test.wantNavigation)
			}
		})
	}
}
