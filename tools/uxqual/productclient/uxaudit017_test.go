package productclient

import (
	"context"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
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

	t.Run("an explicit empty filter clears the saved default", func(t *testing.T) {
		// The "Open work" tab addresses itself as filter= (productui's
		// workFilterHref); that must mean "no filter", not "use the saved one".
		state, err := ParseState("/workspace/app/work", "filter=")
		if err != nil {
			t.Fatal(err)
		}
		view, err := Load(context.Background(), service, Session{Tenant: "tenant", Principal: "priya"}, state)
		if err != nil {
			t.Fatal(err)
		}
		if view.WorkFilter != "" {
			t.Fatalf("WorkFilter = %q, an explicit empty filter must clear the saved %q", view.WorkFilter, "blocked")
		}
	})

	t.Run("My Work and Journeys deep-link to the same canonical intent workspace", func(t *testing.T) {
		const intent = "intent-7f3a"
		linked := service
		linked.ListJourneys = func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			return &journeyv1.ListJourneysResponse{Journeys: []*journeyv1.Journey{{
				IntentId: intent, WorkerRef: "worker-lin", WorkerName: "Lin Park", EffectiveDate: "2026-10-01",
				Stage: journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED,
			}}}, nil
		}
		state, err := ParseState("/workspace/app/work", "filter=blocked")
		if err != nil {
			t.Fatal(err)
		}
		view, err := Load(context.Background(), linked, Session{Tenant: "tenant", Principal: "priya"}, state)
		if err != nil {
			t.Fatal(err)
		}
		if len(view.Work) != 1 {
			t.Fatalf("Work = %+v, want the one journey", view.Work)
		}
		myWork := view.Work[0].Href
		// The Journeys tracker's card links journeyclient.DetailHref; the
		// product router translates that fragment with ProductJourneyHref.
		journeys := ProductJourneyHref(journeyclient.DetailHref(intent), "")
		if myWork != journeys || myWork != "/workspace/app/journeys?journey="+intent {
			t.Fatalf("deep links diverge: My Work %q, Journeys %q", myWork, journeys)
		}
		// The queue item carries the same stage dimension the tracker shows.
		if view.Work[0].NextStep != string(journeyclient.NextStepCorrectProposal) || view.Work[0].WaitingOn != string(journeyclient.StageActorProposer) || !view.Work[0].AwaitsPerson {
			t.Fatalf("My Work item lost the shared status dimension: %+v", view.Work[0])
		}
	})

	t.Run("the server's work item summary reaches My Work, and its absence stays absent", func(t *testing.T) {
		summarized := service
		summarized.ListJourneys = func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			return &journeyv1.ListJourneysResponse{Journeys: []*journeyv1.Journey{
				{IntentId: "with-summary", WorkerRef: "worker-a", WorkerName: "Ana Lim", Stage: journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL,
					CurrentWorkItem: &journeyv1.JourneyWorkItemSummary{
						Kind: "APPROVAL", Status: "ASSIGNED", AssigneePrincipalId: "principal:finance-partner",
						DueAt: timestamppb.New(time.Date(2026, 10, 2, 17, 0, 0, 0, time.UTC)), ViewerPermittedActions: []string{"claim"}, ViewerMembership: "ASSIGNEE",
					}},
				{IntentId: "no-summary", WorkerRef: "worker-b", WorkerName: "Ben Cho", Stage: journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL},
			}}, nil
		}
		state, err := ParseState("/workspace/app/work", "filter=")
		if err != nil {
			t.Fatal(err)
		}
		view, err := Load(context.Background(), summarized, Session{Tenant: "tenant", Principal: "priya"}, state)
		if err != nil {
			t.Fatal(err)
		}
		byID := map[string]productui.WorkItem{}
		for _, item := range view.Work {
			byID[item.ID] = item
		}
		with, without := byID["with-summary"], byID["no-summary"]
		if !with.WorkSummary || with.AssigneeRef != "principal:finance-partner" || with.AssigneeName == "" || with.WorkDue != "2026-10-02" ||
			with.ViewerMembership != "ASSIGNEE" || productui.WorkNextAction(with) != "claim" {
			t.Fatalf("summary not projected: %+v", with)
		}
		if without.WorkSummary || without.AssigneeRef != "" || without.AssigneeName != "" || without.WorkDue != "" || without.ViewerMembership != "" || len(without.PermittedActions) != 0 {
			t.Fatalf("a journey with no summary gained invented work item facts: %+v", without)
		}
		mine, err := ParseState("/workspace/app/work", "filter=mine")
		if err != nil {
			t.Fatalf("the Assigned to me filter is not a valid route: %v", err)
		}
		mineView, err := Load(context.Background(), summarized, Session{Tenant: "tenant", Principal: "priya"}, mine)
		if err != nil {
			t.Fatal(err)
		}
		if len(mineView.Work) != 1 || mineView.Work[0].ID != "with-summary" {
			t.Fatalf("Assigned to me = %+v, want only the journey assigned to the viewer", mineView.Work)
		}
	})

	t.Run("the two pages' empty states are distinct", func(t *testing.T) {
		workView := productui.NewView(productui.PageWork, "tenant", "priya", "")
		myWork, err := productui.Render(workView)
		if err != nil {
			t.Fatal(err)
		}
		journeys, err := journey.RenderToString(journeyclient.ListPage(journeyclient.Config{Tenant: "tenant", Subject: "priya"}, journeyclient.ListData{}, nil, nil))
		if err != nil {
			t.Fatal(err)
		}
		const workTitle, journeysTitle = "Nothing needs your action in this view", "No journeys to track"
		if !strings.Contains(myWork, workTitle) || strings.Contains(myWork, journeysTitle) {
			t.Fatalf("My Work empty state is not the action-queue one")
		}
		if !strings.Contains(journeys, journeysTitle) || strings.Contains(journeys, workTitle) {
			t.Fatalf("Journeys empty state is not the tracker one")
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

	t.Run("productui has copy for exactly the codes the shared dimension produces", func(t *testing.T) {
		for value := range journeyv1.JourneyStage_name {
			dimension := journeyclient.StageStatusDimension(journeyv1.JourneyStage(value))
			if dimension.NextStep != journeyclient.NextStepNone && !productui.KnownWorkNextStep(string(dimension.NextStep)) {
				t.Errorf("stage %d: My Work has no copy for next step %q", value, dimension.NextStep)
			}
			if dimension.WaitingOn != journeyclient.StageActorUnstated && !productui.KnownWorkActor(string(dimension.WaitingOn)) {
				t.Errorf("stage %d: My Work has no copy for actor %q", value, dimension.WaitingOn)
			}
			for _, locale := range productui.SupportedProductLocales() {
				resolved := productui.ResolveProductLocale(locale)
				for _, key := range []string{"work.next_step." + string(dimension.NextStep), "work.waiting_on." + string(dimension.WaitingOn)} {
					if strings.HasSuffix(key, ".") {
						continue
					}
					if text := resolved.Text(key); strings.HasPrefix(text, "⟦") {
						t.Errorf("%s: %s unresolved", locale, key)
					}
				}
			}
		}
		if productui.KnownWorkNextStep("approve_everything") || productui.KnownWorkActor("ceo") {
			t.Fatal("productui accepts a code outside the shared vocabulary")
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
