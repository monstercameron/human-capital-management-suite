package productclient

import (
	"context"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestServerPreferencesBecomeDefaultsButExplicitURLStateWins(t *testing.T) {
	service := Service{
		ListJourneys: func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			return &journeyv1.ListJourneysResponse{}, nil
		},
		ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			return &journeyv1.ListWorkersResponse{}, nil
		},
		GetPreferences: func(context.Context, *journeyv1.GetProductPreferencesRequest) (*journeyv1.GetProductPreferencesResponse, error) {
			return &journeyv1.GetProductPreferencesResponse{User: &journeyv1.UserPreferences{Version: 7, Locale: "de-DE", NavCollapsed: true,
				Tables:       map[string]*journeyv1.TablePreferences{"people": {PageSize: 50, Filters: map[string]string{"team": "Care Operations"}, Sort: "role", Direction: "desc"}},
				WorkflowUses: map[string]int64{"promotion": 9}}, Theme: &journeyv1.CustomerTheme{Version: 3, BrandName: "HarborCare", BrandMark: "HC", Palette: "ocean", ColorMode: "dark", Shape: "rounded", Density: "compact", Glyphs: "bold-line", Typeface: "modern", Navigation: "brand", Motion: "brisk"}}, nil
		},
	}
	state, err := ParseState("/workspace/app/people", "page_size=10&team=")
	if err != nil {
		t.Fatal(err)
	}
	view, err := Load(context.Background(), service, Session{Tenant: "tenant", Principal: "alice"}, state)
	if err != nil {
		t.Fatal(err)
	}
	if view.PeoplePageSize != 10 || view.PeopleTeam != "" {
		t.Fatalf("explicit URL state lost: size=%d team=%q", view.PeoplePageSize, view.PeopleTeam)
	}
	if view.PeopleSort != "role" || view.PeopleDirection != "desc" {
		t.Fatalf("stored sort not applied: %s %s", view.PeopleSort, view.PeopleDirection)
	}
	if view.Locale.Resolved != "de-DE" || !view.NavCollapsed || view.Appearance.BrandName != "HarborCare" {
		t.Fatalf("stored presentation not applied: locale=%s nav=%v theme=%+v", view.Locale.Resolved, view.NavCollapsed, view.Appearance)
	}
	if view.WorkflowUses["promotion"] != 9 || view.StoredPreferences.Version != 7 {
		t.Fatalf("usage/baseline missing: %+v", view)
	}
}

func TestTodo_UXAUDIT_024_HydratedBrandKeepsTenantIdentity(t *testing.T) {
	theme := &journeyv1.CustomerTheme{}
	service := Service{
		ListJourneys: func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			return &journeyv1.ListJourneysResponse{}, nil
		},
		ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			return &journeyv1.ListWorkersResponse{}, nil
		},
		GetPreferences: func(context.Context, *journeyv1.GetProductPreferencesRequest) (*journeyv1.GetProductPreferencesResponse, error) {
			return &journeyv1.GetProductPreferencesResponse{Theme: theme}, nil
		},
	}
	session := Session{Tenant: "harborcare-demo", Principal: "rafael"}
	state, err := ParseState("/workspace/app/home", "")
	if err != nil {
		t.Fatal(err)
	}
	loading := LoadingView(session, state)
	view, err := Load(context.Background(), service, session, state)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []productui.View{loading, view} {
		name, mark := productui.HeaderBrandIdentity(productui.NormalizeCustomerTheme(candidate.Appearance), candidate.Tenant)
		if name != "Harborcare Demo" || mark != "HD" {
			t.Fatalf("loading/resolved identity diverged: tenant=%q name=%q mark=%q", candidate.Tenant, name, mark)
		}
	}
	theme.BrandName, theme.BrandMark, theme.BrandLogoUrl = "Northstar People", "NP", "/workspace/assets/northstar.svg"
	branded, err := Load(context.Background(), service, session, state)
	if err != nil {
		t.Fatal(err)
	}
	if name, mark := productui.HeaderBrandIdentity(branded.Appearance, branded.Tenant); name != "Northstar People" || mark != "NP" || branded.Appearance.BrandLogoURL != theme.BrandLogoUrl {
		t.Fatalf("explicit configured brand lost: name=%q mark=%q logo=%q", name, mark, branded.Appearance.BrandLogoURL)
	}
}

func TestExplicitSortColumnDoesNotInheritStaleSavedDirection(t *testing.T) {
	service := Service{
		ListJourneys: func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			return &journeyv1.ListJourneysResponse{}, nil
		},
		ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			return &journeyv1.ListWorkersResponse{}, nil
		},
		GetPreferences: func(context.Context, *journeyv1.GetProductPreferencesRequest) (*journeyv1.GetProductPreferencesResponse, error) {
			return &journeyv1.GetProductPreferencesResponse{User: &journeyv1.UserPreferences{Tables: map[string]*journeyv1.TablePreferences{
				"people":  {Sort: "manager", Direction: "desc"},
				"history": {Sort: "closed", Direction: "desc"},
			}}}, nil
		},
	}

	peopleState, err := ParseState("/workspace/app/people", "sort=role")
	if err != nil {
		t.Fatal(err)
	}
	people, err := Load(context.Background(), service, Session{}, peopleState)
	if err != nil {
		t.Fatal(err)
	}
	if people.PeopleSort != "role" || people.PeopleDirection != "asc" {
		t.Fatalf("first people sort click inherited saved direction: sort=%q direction=%q", people.PeopleSort, people.PeopleDirection)
	}

	historyState, err := ParseState("/workspace/app/history", "history_sort=person")
	if err != nil {
		t.Fatal(err)
	}
	history, err := Load(context.Background(), service, Session{}, historyState)
	if err != nil {
		t.Fatal(err)
	}
	if history.HistorySort != "person" || history.HistoryDirection == "desc" {
		t.Fatalf("first history sort click inherited saved direction: sort=%q direction=%q", history.HistorySort, history.HistoryDirection)
	}
}
