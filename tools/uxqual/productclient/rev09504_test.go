package productclient

import (
	"context"
	"strings"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// REV-095-04: directory search is page-scoped. A stored People filter does not
// leak into Organization, while an explicit Organization filter remains
// shareable and clearing it stays cleared.

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

	// People -> Organization with no search in the address does not carry the
	// stored People-table filter.
	state, view := rev09504Load(t, service, organization, "")
	if view.Query != "" {
		t.Fatalf("cross-page search = %q, want empty", view.Query)
	}
	href := ResolvedCanonicalHref(state, view)
	if href != organization {
		t.Fatalf("Organization address = %q, want no query", href)
	}
	if state.Provided["q"] {
		t.Fatal("resolving the address mutated the caller's parsed state")
	}

	if len(view.People) != 2 {
		t.Fatalf("Organization population = %d, want unfiltered population", len(view.People))
	}

	// An explicit Organization search is page-local and shareable.
	sharedState, shared := rev09504Load(t, rev09504Service(""), organization, "q=Jane")
	sharedHref := ResolvedCanonicalHref(sharedState, shared)
	if shared.Query != "Jane" || sharedHref != organization+"?q=Jane" {
		t.Fatalf("explicit Organization search = %q -> %q", shared.Query, sharedHref)
	}
	sharedPath, sharedQuery, _ := strings.Cut(sharedHref, "?")
	reloadState, reloaded := rev09504Load(t, rev09504Service(""), sharedPath, sharedQuery)
	if reloaded.Query != "Jane" || !reloadState.Provided["q"] || ResolvedCanonicalHref(reloadState, reloaded) != sharedHref {
		t.Fatalf("shared address loaded query %q (provided %v), want stable Jane", reloaded.Query, reloadState.Provided["q"])
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

	// Organization no longer declares a cross-page directory-query carry.
	homeState, homeView := rev09504Load(t, service, productui.Path(productui.PageHome), "")
	if got := ResolvedCanonicalHref(homeState, homeView); strings.Contains(got, "q=") {
		t.Fatalf("Home gained a search it does not declare: %q", got)
	}
	profile, _, _ := productui.PageProfiles(productui.PageOrganization)
	if profile.StateProfile().CarriesDirectoryQuery {
		t.Fatal("Organization's route-state profile still declares a carried search")
	}
}

func TestTodo_UXBLIND_079(t *testing.T) {
	organization := productui.Path(productui.PageOrganization)
	state, view := rev09504Load(t, rev09504Service("Omar"), organization, "")
	if view.Query != "" || ResolvedCanonicalHref(state, view) != organization {
		t.Fatalf("stored People search leaked into Organization: query=%q href=%q", view.Query, ResolvedCanonicalHref(state, view))
	}
	cleared, clearedView := rev09504Load(t, rev09504Service("Omar"), organization, "q=")
	if clearedView.Query != "" || ResolvedCanonicalHref(cleared, clearedView) != organization+"?q=" {
		t.Fatalf("explicit clear did not stick: query=%q href=%q", clearedView.Query, ResolvedCanonicalHref(cleared, clearedView))
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
		t.Fatal("the organization search field does not show its explicit search")
	}
}

func TestTodo_UXBLIND_079_Browser(t *testing.T) {
	organization := productui.Path(productui.PageOrganization)
	_, docView := rev09504Load(t, rev09504Service("Jane"), organization, "")
	doc, err := productui.Render(docView)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "Jane Doe") || !strings.Contains(doc, "Omar Reyes") || strings.Contains(doc, `value="Jane"`) {
		t.Fatal("stored People query changed the unscoped Organization render")
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
