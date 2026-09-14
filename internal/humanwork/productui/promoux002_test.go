package productui

import "testing"

// TestTodo_PROMOUX_002_Browser is the BROWSER matrix entry. GREEN's third
// clause is "People and Person replace Start with `Open active promotion`,
// linking to the existing journey" -- a claim about what the browser shows,
// not about what the server refused.
//
// The People directory's Actions column is rendered client-side from row
// props (tools/uxqual/productclient projects PeopleRowProps, and the SSR
// document never carries it), so this pins the contract on peopleRowProps'
// output rather than on Render()'s markup, exactly as
// TestTodo_PROMOUX_001_Browser already does for the same column. Person's
// workflow launcher is server-rendered, so that half is asserted on
// personWorkflowLauncherProps directly, which is what both Render() and any
// client hydration ultimately read.
func TestTodo_PROMOUX_002_Browser(t *testing.T) {
	activeJourneyHref := func(view View, intentID string) string { return JourneyDetailHref(view, intentID) }

	t.Run("People row replaces Start with a link to the existing journey", func(t *testing.T) {
		view := testView(PagePeople)
		view.PersonWorkflows = []PersonWorkflow{{ID: "promotion", Name: "Promotion", LaunchHref: func(id string) string { return "/workspace/app/journeys?mode=new&worker=" + id }}}
		view.People = []Person{
			{ID: "worker-conflict", Name: "Conflicted Worker", PromotionAvailability: PromotionActiveConflict},
			{ID: "worker-eligible", Name: "Eligible Worker", PromotionAvailability: PromotionEligible},
		}
		view.Work = []WorkItem{
			{ID: "intent-active-1", PersonRef: "worker-conflict", Terminal: false},
			// A terminal journey for the same worker must never be offered as
			// "the" active one -- Terminal:true rows are history, not what is
			// blocking a new promotion.
			{ID: "intent-done-1", PersonRef: "worker-conflict", Terminal: true},
		}
		rows := peopleRowProps(view, peoplePageWindow{People: view.People, Total: 2, PageCount: 1, Page: 1})
		if len(rows) != 2 {
			t.Fatalf("rows = %d, want 2", len(rows))
		}
		conflicted := rows[0]
		if len(conflicted.QuickActions) != 1 {
			t.Fatalf("conflicted worker's row = %+v, want exactly one quick action (Open active promotion)", conflicted)
		}
		action := conflicted.QuickActions[0]
		wantLabel := view.Locale.Text("people.open_active_promotion")
		if action.Label != wantLabel {
			t.Errorf("conflicted worker's action label = %q, want %q (Start must be replaced, not merely relabeled)", action.Label, wantLabel)
		}
		wantHref := activeJourneyHref(view, "intent-active-1")
		if action.Href != wantHref {
			t.Errorf("conflicted worker's action href = %q, want the ACTIVE journey's own %q", action.Href, wantHref)
		}
		if action.Href == activeJourneyHref(view, "intent-done-1") {
			t.Error("conflicted worker's action links to the terminal journey, not the active one")
		}
		// This is a courtesy on top of the guard, not the guard itself
		// (REFACTOR): the row must not claim to launch a new promotion.
		for _, a := range conflicted.QuickActions {
			if a.Href == "/workspace/app/journeys?mode=new&worker=worker-conflict" {
				t.Error("conflicted worker's row still offers the Start-a-new-promotion href")
			}
		}

		eligible := rows[1]
		if len(eligible.QuickActions) != 1 || eligible.QuickActions[0].Label != "Promotion" {
			t.Fatalf("eligible worker's own row must be unaffected: %+v", eligible)
		}
	})

	t.Run("People row falls back to the reason when no matching journey is in view.Work", func(t *testing.T) {
		// A Person carrying PromotionActiveConflict with nothing in view.Work
		// to point at (a fixture that only set the availability code) must
		// not panic or link nowhere; it renders the existing reason text
		// instead, exactly as it did before this todo.
		view := testView(PagePeople)
		view.PersonWorkflows = []PersonWorkflow{{ID: "promotion", Name: "Promotion", LaunchHref: func(id string) string { return "/workspace/app/journeys?mode=new&worker=" + id }}}
		view.People = []Person{{ID: "worker-conflict-orphan", Name: "Orphan Conflict", PromotionAvailability: PromotionActiveConflict}}
		rows := peopleRowProps(view, peoplePageWindow{People: view.People, Total: 1, PageCount: 1, Page: 1})
		if len(rows) != 1 {
			t.Fatalf("rows = %d, want 1", len(rows))
		}
		if len(rows[0].QuickActions) != 0 {
			t.Fatalf("no active journey was supplied; want zero quick actions, got %+v", rows[0].QuickActions)
		}
		want := PromotionAvailabilityReason(view.Locale, PromotionActiveConflict)
		if rows[0].WorkflowsUnavailableReason != want {
			t.Errorf("fallback reason = %q, want %q", rows[0].WorkflowsUnavailableReason, want)
		}
	})

	t.Run("Person's workflow launcher replaces the promotion card with Open active promotion", func(t *testing.T) {
		view := testView(PagePerson)
		view.EffectivePermissions = []RolePagePermission{{Page: PageJourneys, View: true, Create: true}}
		view.PersonWorkflows = []PersonWorkflow{{ID: "promotion", Name: "Promotion", Category: "People", LaunchHref: func(id string) string { return "/workspace/app/journeys?mode=new&worker=" + id }}}
		view.Work = []WorkItem{{ID: "intent-active-2", PersonRef: "worker-conflict", Terminal: false}}
		person := Person{ID: "worker-conflict", Name: "Conflicted Worker", PromotionAvailability: PromotionActiveConflict}

		props := personWorkflowLauncherProps(view, person, PagePerson)
		if props.Heading != view.Locale.Text("workflow.continue_heading") || props.Description == "" || !props.HideCount {
			t.Fatalf("active request still framed as a new workflow: %+v", props)
		}
		if props.UnavailableDetail != "" {
			t.Errorf("UnavailableDetail = %q, want empty -- the Open active promotion card already carries continuity", props.UnavailableDetail)
		}
		found := false
		for _, card := range props.Workflows {
			if card.Href == "/workspace/app/journeys?mode=new&worker=worker-conflict" {
				t.Fatal("Person still offers the Start-a-new-promotion href for a conflicted worker")
			}
			if card.Name == view.Locale.Text("people.open_active_promotion") {
				found = true
				if card.ActionLabel != card.Name {
					t.Fatalf("active request link still says Start: %+v", card)
				}
				if card.Href != JourneyDetailHref(view, "intent-active-2") {
					t.Errorf("Open active promotion card href = %q, want the active journey's own", card.Href)
				}
			}
		}
		if !found {
			t.Fatalf("no Open active promotion card rendered; workflows = %+v", props.Workflows)
		}
	})
}
