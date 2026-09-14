package productclient

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func TestFirstClassJourneyRouteTranslatesToLiveClientFragments(t *testing.T) {
	tests := []struct {
		request productui.PageRequest
		want    string
	}{
		{request: productui.PageRequest{}, want: journeyclient.ListHref()},
		{request: productui.PageRequest{JourneyWorker: "worker/a+b"}, want: journeyclient.WorkerHref("worker/a+b")},
		{request: productui.PageRequest{JourneyMode: "new", JourneyWorker: "worker/a+b"}, want: journeyclient.ProposalHref("worker/a+b")},
		{request: productui.PageRequest{JourneyID: "intent-17", JourneyMode: "new", JourneyWorker: "ignored"}, want: journeyclient.DetailHref("intent-17")},
	}
	for _, test := range tests {
		if got := JourneyFragment(test.request); got != test.want {
			t.Errorf("JourneyFragment(%+v) = %q, want %q", test.request, got, test.want)
		}
	}
}

func TestLiveJourneyNavigationStaysInProductRouterAndPreservesShellState(t *testing.T) {
	tests := []struct {
		fragment string
		want     string
	}{
		{journeyclient.ListHref(), "/workspace/app/journeys?favorites=people%2Chistory&locale=de-DE&menu_q=journey&nav=collapsed"},
		{journeyclient.DetailHref("intent+17"), "/workspace/app/journeys?favorites=people%2Chistory&journey=intent%2B17&locale=de-DE&menu_q=journey&nav=collapsed"},
		{journeyclient.ProposalHref("worker/a+b"), "/workspace/app/journeys?favorites=people%2Chistory&locale=de-DE&menu_q=journey&mode=new&nav=collapsed&worker=worker%2Fa%2Bb"},
	}
	for _, test := range tests {
		got := ProductJourneyHref(test.fragment, "nav=collapsed&locale=de-DE&menu_q=journey&favorites=people,history,unknown,people&journey=stale&worker=stale")
		if got != test.want {
			t.Errorf("ProductJourneyHref(%q) = %q, want %q", test.fragment, got, test.want)
		}
	}
}

func TestTodo_UIPOLISH_009_FragmentFallbackRoutesToProductDetail(t *testing.T) {
	const query = "favorites=people&nav=collapsed&credential=secret"
	tests := []struct {
		name, path, fragment, want string
		ok                         bool
	}{
		{"detail", "/workspace/app/journeys", "#/journeys/intent-17", "/workspace/app/journeys?favorites=people&journey=intent-17&nav=collapsed", true},
		{"proposal", "/workspace/app/journeys", "#/journeys/new?worker=jane-doe", "/workspace/app/journeys?favorites=people&mode=new&nav=collapsed&worker=jane-doe", true},
		{"list", "/workspace/app/journeys", "#/journeys", "/workspace/app/journeys?favorites=people&nav=collapsed", true},
		{"unrelated page", "/workspace/app/people", "#/journeys/intent-17", "", false},
		{"skip link", "/workspace/app/journeys", "#main-content", "", false},
		{"similar prefix", "/workspace/app/journeys", "#/journeys-other", "", false},
		{"extra state", "/workspace/app/journeys", "#/journeys/intent-17?credential=secret", "", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := ProductJourneyHashHref(test.path, test.fragment, query)
			if got != test.want || ok != test.ok {
				t.Fatalf("hash route = (%q, %t), want (%q, %t)", got, ok, test.want, test.ok)
			}
		})
	}
}

func TestParseStateReadsJourneyAddressState(t *testing.T) {
	state, err := ParseState("/workspace/app/journeys", "journey=intent-17&worker=worker-1&mode=new&nav=collapsed")
	if err != nil {
		t.Fatal(err)
	}
	if state.Page != productui.PageJourneys || state.Request.JourneyID != "intent-17" || state.Request.JourneyWorker != "" || state.Request.JourneyMode != "" || !state.Request.NavCollapsed {
		t.Fatalf("journey state = %+v", state)
	}
}
