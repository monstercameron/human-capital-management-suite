package productclient

import (
	"context"
	"strings"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// REV-095-04: a People directory search carried into Organization (the
// stored people-table filter the preference layer applies when the address
// names no search) is written into Organization's own address, so reloading
// or sharing it reproduces the filtered browse while the scope counts stay
// unfiltered, and Back returns to an unchanged People entry.

func rev09504Service(storedQuery string) Service {
	return Service{
		ListJourneys: func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			return &journeyv1.ListJourneysResponse{}, nil
		},
		ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			return &journeyv1.ListWorkersResponse{Workers: []*journeyv1.Worker{
				{WorkerRef: "jane", LegalName: "Jane Doe", OrgUnit: "Engineering Platform", Location: "San Francisco, CA", PayZone: "US-WEST", ManagerRelationship: rootManagerRelationship()},
				{WorkerRef: "omar", LegalName: "Omar Reyes", OrgUnit: "People Operations", Location: "Boston, MA", PayZone: "US-EAST", ManagerRelationship: rootManagerRelationship()},
			}}, nil
		},
		GetPreferences: func(context.Context, *journeyv1.GetProductPreferencesRequest) (*journeyv1.GetProductPreferencesResponse, error) {
			user := &journeyv1.UserPreferences{Version: 1}
			if storedQuery != "" {
				user.Tables = map[string]*journeyv1.TablePreferences{"people": {Filters: map[string]string{"query": storedQuery}}}
			}
			return &journeyv1.GetProductPreferencesResponse{User: user}, nil
		},
	}
}

func rev09504Load(t *testing.T, service Service, path, query string) (State, productui.View) {
	t.Helper()
	state, err := ParseState(path, query)
	if err != nil {
		t.Fatal(err)
	}
	view, err := Load(context.Background(), service, Session{Tenant: "tenant", Principal: "alice"}, state)
	if err != nil {
		t.Fatal(err)
	}
	return state, view
}

func TestTodo_REV_095_04(t *testing.T) {
	service := rev09504Service("Jane")
	organization := productui.Path(productui.PageOrganization)

	// People -> Organization with no search in the address.
	state, view := rev09504Load(t, service, organization, "")
	if view.Query != "Jane" {
		t.Fatalf("carried search = %q, want Jane (the premise of this todo)", view.Query)
	}
	href := ResolvedCanonicalHref(state, view)
	if href != organization+"?q=Jane" {
		t.Fatalf("Organization address = %q, want the carried search in it", href)
	}
	if state.Provided["q"] {
		t.Fatal("resolving the address mutated the caller's parsed state")
	}

	// Reload/share: the address alone reproduces the same browse, even for a
	// viewer with no stored search at all.
	sharedPath, sharedQuery, _ := strings.Cut(href, "?")
	reloadState, reloaded := rev09504Load(t, rev09504Service(""), sharedPath, sharedQuery)
	if reloaded.Query != "Jane" || !reloadState.Provided["q"] {
		t.Fatalf("shared address loaded query %q (provided %v), want Jane", reloaded.Query, reloadState.Provided["q"])
	}
	if got := ResolvedCanonicalHref(reloadState, reloaded); got != href {
		t.Fatalf("reloaded address = %q, want the stable %q", got, href)
	}
	if len(reloaded.People) != len(view.People) {
		t.Fatalf("scope population differs: %d vs %d", len(reloaded.People), len(view.People))
	}

	// An explicit clear stays a clear; nothing is carried over it.
	cleared, clearedView := rev09504Load(t, service, organization, "q=")
	if clearedView.Query != "" || ResolvedCanonicalHref(cleared, clearedView) != organization+"?q=" {
		t.Fatalf("explicit clear = %q -> %q", clearedView.Query, ResolvedCanonicalHref(cleared, clearedView))
	}

	// Back returns to People: its own entry is not rewritten by Organization.
	peopleState, peopleView := rev09504Load(t, service, productui.Path(productui.PagePeople), "q=Jane")
	if got := ResolvedCanonicalHref(peopleState, peopleView); got != productui.Path(productui.PagePeople)+"?q=Jane" {
		t.Fatalf("People address = %q", got)
	}

	// Only pages that declare the carry in their route-state profile get it.
	homeState, homeView := rev09504Load(t, service, productui.Path(productui.PageHome), "")
	if got := ResolvedCanonicalHref(homeState, homeView); strings.Contains(got, "q=") {
		t.Fatalf("Home gained a search it does not declare: %q", got)
	}
	profile, _, _ := productui.PageProfiles(productui.PageOrganization)
	if !profile.StateProfile().CarriesDirectoryQuery {
		t.Fatal("Organization's route-state profile does not declare the carried search")
	}
}

// TestTodo_REV_095_04_Browser renders the organization page from the shared
// address: the browse groups are the search result and the scope card keeps
// the unfiltered counts.
func TestTodo_REV_095_04_Browser(t *testing.T) {
	organization := productui.Path(productui.PageOrganization)
	_, filtered := rev09504Load(t, rev09504Service(""), organization, "q=Jane")
	_, whole := rev09504Load(t, rev09504Service(""), organization, "q=")
	filteredDoc, err := productui.Render(filtered)
	if err != nil {
		t.Fatal(err)
	}
	wholeDoc, err := productui.Render(whole)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(filteredDoc, "Jane Doe") || strings.Contains(filteredDoc, "Omar Reyes") {
		t.Fatal("the shared address did not reproduce the filtered browse")
	}
	units := filtered.Locale.Text("organization.units")
	if rev09504Metadata(t, filteredDoc, units) != rev09504Metadata(t, wholeDoc, units) {
		t.Fatalf("scope %q changed under the carried search", units)
	}
	if !strings.Contains(filteredDoc, `value="Jane"`) {
		t.Fatal("the organization search field does not show the carried search")
	}
}

func rev09504Metadata(t *testing.T, doc, label string) string {
	t.Helper()
	marker := ">" + label + "</dt><dd"
	at := strings.Index(doc, marker)
	if at < 0 {
		t.Fatalf("no %q row", label)
	}
	rest := doc[at+len(marker):]
	open, closing := strings.Index(rest, ">"), strings.Index(rest, "</dd>")
	if open < 0 || closing < 0 {
		t.Fatalf("malformed %q row", label)
	}
	return rest[open+1 : closing]
}
