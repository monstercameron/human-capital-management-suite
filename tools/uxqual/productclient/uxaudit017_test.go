package productclient

import (
	"context"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_UXAUDIT_017 is this package's contribution to the PRIMARY
// contract: UXAUDIT-017 GREEN requires My Work to "retain filters on
// return", and the plan directs reusing the People/History table
// preference mechanism (UXAUDIT-008) rather than inventing a new one. This
// is the exact round trip GREEN describes -- set a filter, navigate away,
// come back with no query -- driven through the same Load path a real
// navigation uses, mirroring
// TestServerPreferencesBecomeDefaultsButExplicitURLStateWins's existing
// pattern for the People table.
func TestTodo_UXAUDIT_017(t *testing.T) {
	service := Service{
		ListJourneys: func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			return &journeyv1.ListJourneysResponse{}, nil
		},
		ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			return &journeyv1.ListWorkersResponse{}, nil
		},
		GetPreferences: func(context.Context, *journeyv1.GetProductPreferencesRequest) (*journeyv1.GetProductPreferencesResponse, error) {
			return &journeyv1.GetProductPreferencesResponse{User: &journeyv1.UserPreferences{
				Tables: map[string]*journeyv1.TablePreferences{"work": {Filters: map[string]string{"filter": "blocked"}}},
			}}, nil
		},
	}

	t.Run("returning to My Work with no query adopts the saved filter", func(t *testing.T) {
		state, err := ParseState("/workspace/app/work", "")
		if err != nil {
			t.Fatal(err)
		}
		view, err := Load(context.Background(), service, Session{Tenant: "tenant", Principal: "priya"}, state)
		if err != nil {
			t.Fatal(err)
		}
		if view.WorkFilter != "blocked" {
			t.Fatalf("WorkFilter = %q, want the saved %q to survive the return with no query", view.WorkFilter, "blocked")
		}
	})

	t.Run("an explicit URL filter still wins over the saved default", func(t *testing.T) {
		state, err := ParseState("/workspace/app/work", "filter=review")
		if err != nil {
			t.Fatal(err)
		}
		view, err := Load(context.Background(), service, Session{Tenant: "tenant", Principal: "priya"}, state)
		if err != nil {
			t.Fatal(err)
		}
		if view.WorkFilter != "review" {
			t.Fatalf("WorkFilter = %q, want the explicit URL value %q, not the saved %q", view.WorkFilter, "review", "blocked")
		}
	})
}

// TestTodo_UXAUDIT_017_Regression pins applyWorkTableDefaults directly: a
// nil table must never panic or mutate the request, an explicitly-provided
// filter must never be overwritten, and a table with no "filter" entry must
// not invent one.
func TestTodo_UXAUDIT_017_Regression(t *testing.T) {
	t.Run("nil table leaves the request untouched", func(t *testing.T) {
		request := &productui.PageRequest{WorkFilter: "review"}
		applyWorkTableDefaults(request, map[string]bool{}, nil)
		if request.WorkFilter != "review" {
			t.Fatalf("WorkFilter = %q, a nil table must not change it", request.WorkFilter)
		}
	})

	t.Run("provided filter is never overwritten by the stored default", func(t *testing.T) {
		request := &productui.PageRequest{WorkFilter: "review"}
		table := &journeyv1.TablePreferences{Filters: map[string]string{"filter": "blocked"}}
		applyWorkTableDefaults(request, map[string]bool{"filter": true}, table)
		if request.WorkFilter != "review" {
			t.Fatalf("WorkFilter = %q, an explicitly provided filter must win over the stored %q", request.WorkFilter, "blocked")
		}
	})

	t.Run("a table with no filter entry does not invent one", func(t *testing.T) {
		request := &productui.PageRequest{}
		table := &journeyv1.TablePreferences{Filters: map[string]string{}}
		applyWorkTableDefaults(request, map[string]bool{}, table)
		if request.WorkFilter != "" {
			t.Fatalf("WorkFilter = %q, want empty when the stored table names no filter", request.WorkFilter)
		}
	})
}
