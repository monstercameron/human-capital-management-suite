package productclient

import (
	"net/url"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// UXLIVE-031 puts the Journeys tracker's search, status, date range, sort
// and grouping in the canonical product address. These tests walk the
// served path: the product router parses the address, hands the live
// journey client its fragment, and turns the client's navigation back into
// the same canonical address, so reload, a copied link and Back agree.

// TestTodo_UXLIVE_031_Integration round-trips a filtered tracker address
// through the product router and the live journey client.
func TestTodo_UXLIVE_031_Integration(t *testing.T) {
	path := productui.Path(productui.PageJourneys)
	query := "journey_q=Amara&journey_status=review&journey_from=2026-09-01&journey_to=2026-09-30&journey_sort=oldest&journey_group=none&nav=collapsed"
	state, err := ParseState(path, query)
	if err != nil {
		t.Fatalf("a filtered tracker address was refused: %v", err)
	}
	want := productui.JourneyListFilter{Query: "Amara", Status: "review", From: "2026-09-01", To: "2026-09-30", Sort: "oldest", Group: "none"}
	if state.Request.JourneyList != want {
		t.Fatalf("parsed filter = %+v, want %+v", state.Request.JourneyList, want)
	}
	canonical := CanonicalHref(state)
	fragment := JourneyFragment(state.Request)
	if route := journeyclient.Parse(fragment); route.Kind != journeyclient.RouteList || route.Filter != want {
		t.Fatalf("the live client receives %q (%+v)", fragment, route)
	}
	back := ProductJourneyHref(fragment, "nav=collapsed")
	if back != canonical {
		t.Fatalf("the client's navigation %q is not the canonical address %q", back, canonical)
	}
	for _, key := range productui.JourneyListRouteKeys() {
		if !strings.Contains(canonical, key+"=") {
			t.Fatalf("the canonical address dropped %s: %q", key, canonical)
		}
	}

	// Defaults have no spelling, and a detail address carries no list state.
	plain, err := ParseState(path, "journey_sort=recent&journey_group=person")
	if err != nil || CanonicalHref(plain) != path {
		t.Fatalf("default tracker state has a spelling: %q (%v)", CanonicalHref(plain), err)
	}
	detail, err := ParseState(path, "journey=int-1&journey_q=Amara")
	if err != nil || detail.Request.JourneyList != (productui.JourneyListFilter{}) || strings.Contains(CanonicalHref(detail), "journey_q") {
		t.Fatalf("a journey detail carries list filters: %+v (%v)", detail.Request.JourneyList, err)
	}
	// Opening a card from a filtered list goes to the detail; Back is the
	// browser returning to the filtered address, which parses to the same list.
	if href := ProductJourneyHref(journeyclient.DetailHref("int-1"), query); strings.Contains(href, "journey_q") {
		t.Fatalf("opening a journey carried the list filter into its address: %q", href)
	}
	restored, _ := ParseState(path, strings.TrimPrefix(canonical, path+"?"))
	if restored.Request.JourneyList != want {
		t.Fatalf("Back to the filtered address restored %+v", restored.Request.JourneyList)
	}
}

// TestTodo_UXLIVE_031_Security keeps the address an untrusted boundary: a
// value outside the vocabulary is refused rather than guessed, and the
// filter can never carry authority into the router.
func TestTodo_UXLIVE_031_Security(t *testing.T) {
	path := productui.Path(productui.PageJourneys)
	for _, query := range []string{"journey_status=all-tenants", "journey_group=tenant", "journey_from=not-a-date", "journey_q=a&journey_q=b"} {
		if _, err := ParseState(path, query); err == nil {
			t.Errorf("malformed tracker state %q was admitted", query)
		}
	}
	state, err := ParseState(path, "journey_q="+url.QueryEscape("<script>")+"&token=secret")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(CanonicalHref(state), "token") {
		t.Fatalf("an unknown key survived canonicalisation: %q", CanonicalHref(state))
	}
	people, err := ParseState(productui.Path(productui.PagePeople), "journey_q=Amara")
	if err != nil || people.Request.JourneyList != (productui.JourneyListFilter{}) {
		t.Fatalf("the tracker's filter leaked onto People: %+v (%v)", people.Request.JourneyList, err)
	}
}
