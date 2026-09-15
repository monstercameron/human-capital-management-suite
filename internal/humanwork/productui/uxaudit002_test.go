package productui

import (
	"strings"
	"testing"
)

// TestTodo_UXAUDIT_002_Accessibility is the ACCESSIBILITY matrix test for
// planning/todos.md's UXAUDIT-002. The one discoverable promotion path must
// be operable by assistive technology from both ends that expose it: the
// People directory row and the Person profile launcher. Both controls carry
// a non-empty accessible name that identifies the worker the action is
// about, and a viewer who is refused the action still gets the refusal as
// text -- never a control with no name and never silence.
// uxaudit002PromotionHref is the one promotion action the catalogue offers
// personID: the shared target both surfaces must expose. Selecting by the
// catalogue entry (not by "any href naming the worker") keeps the directory
// and profile assertions about the promotion action even when the catalogue
// carries other workflows for the same worker.
func uxaudit002PromotionHref(view View, personID string) string {
	for _, workflow := range view.PersonWorkflows {
		if workflow.ID != "promotion" {
			continue
		}
		if workflow.LaunchHref != nil {
			return workflow.LaunchHref(personID)
		}
		return workflow.Href
	}
	return ""
}

func TestTodo_UXAUDIT_002_Accessibility(t *testing.T) {
	view := testView(PagePeople)
	person := view.People[1] // worker-avery, the eligible fixture worker.
	if person.PromotionAvailability != PromotionEligible {
		t.Fatalf("fixture worker %s availability = %q, want eligible", person.ID, person.PromotionAvailability)
	}
	want := uxaudit002PromotionHref(view, person.ID)
	if want == "" {
		t.Fatal("fixture catalogue offers no promotion action")
	}
	workflows := rankedPersonWorkflows(view.PersonWorkflows, view.WorkflowUses)

	rowActions, _, _ := personWorkflowActions(view, person, workflows)
	var row *PeopleQuickActionProps
	for i := range rowActions {
		if rowActions[i].Href == want {
			row = &rowActions[i]
			break
		}
	}
	if row == nil {
		t.Fatalf("eligible worker %s has no directory promotion action to name", person.ID)
	}
	if strings.TrimSpace(row.AccessibleLabel) == "" {
		t.Errorf("directory promotion action carries no accessible name")
	}
	if !strings.Contains(row.AccessibleLabel, "Avery Patel") {
		t.Errorf("directory promotion action name = %q, want it to identify the worker", row.AccessibleLabel)
	}

	launcher := personWorkflowLauncherProps(view, person, PagePerson)
	var card *WorkflowCardProps
	for i := range launcher.Workflows {
		if launcher.Workflows[i].Href == want {
			card = &launcher.Workflows[i]
			break
		}
	}
	if card == nil {
		t.Fatalf("eligible worker %s has no profile promotion action to name", person.ID)
	}
	if strings.TrimSpace(card.AccessibleLabel) == "" {
		t.Errorf("profile promotion action carries no accessible name")
	}
	if !strings.Contains(card.AccessibleLabel, "Avery Patel") {
		t.Errorf("profile promotion action name = %q, want it to identify the worker", card.AccessibleLabel)
	}

	// The two ends name the same worker for the same action: a screen-reader
	// user hears one path, not two divergent ones.
	if row.AccessibleLabel != card.AccessibleLabel {
		t.Errorf("directory name = %q, profile name = %q, want the same accessible name for the same action", row.AccessibleLabel, card.AccessibleLabel)
	}

	// A refused viewer gets the refusal as text on both surfaces.
	denied := testView(PagePeople)
	denied.EffectivePermissions = []RolePagePermission{{Page: PageJourneys, View: true, Create: false}}
	deniedRows := peopleRowProps(denied, peoplePageWindow{People: []Person{person}, Page: 1, PageCount: 1, Total: 1})
	if len(deniedRows) != 1 {
		t.Fatalf("denied directory rows = %d, want 1", len(deniedRows))
	}
	if len(deniedRows[0].QuickActions) != 0 {
		t.Errorf("denied viewer is offered %d directory actions, want none", len(deniedRows[0].QuickActions))
	}
	if strings.TrimSpace(deniedRows[0].WorkflowsUnavailableReason) == "" {
		t.Errorf("denied directory row carries no refusal text")
	}
	deniedLauncher := personWorkflowLauncherProps(denied, person, PagePerson)
	if len(deniedLauncher.Workflows) != 0 {
		t.Errorf("denied viewer is offered %d profile actions, want none", len(deniedLauncher.Workflows))
	}
	if strings.TrimSpace(deniedLauncher.UnavailableDetail) == "" {
		t.Errorf("denied profile launcher carries no refusal text")
	}
	if deniedRows[0].WorkflowsUnavailableReason != deniedLauncher.UnavailableDetail {
		t.Errorf("directory refusal = %q, profile refusal = %q, want the identical reason on both surfaces",
			deniedRows[0].WorkflowsUnavailableReason, deniedLauncher.UnavailableDetail)
	}
}

// TestTodo_UXAUDIT_002_Regression is the REGRESSION matrix test. It pins
// the three invariants the audit's GREEN depends on every day it ships:
//
//  1. People and Person expose the same promotion action: the directory
//     quick action and the profile launcher card resolve to the identical
//     href for an eligible worker, so neither surface keeps its own
//     page-specific inventory.
//  2. A terminal outcome stays visible: the completed journey remains in
//     My Work (ranked below open work, never dropped) and in the
//     completed-work history, while the actionable badge counts only open
//     work.
//  3. Availability fails closed: a worker with no recorded verdict is
//     never offered a launchable promotion action.
func TestTodo_UXAUDIT_002_Regression(t *testing.T) {
	t.Run("directory and profile expose the same promotion action", func(t *testing.T) {
		view := testView(PagePeople)
		person := view.People[1] // worker-avery, eligible, no open journey.
		want := uxaudit002PromotionHref(view, person.ID)
		if want == "" {
			t.Fatal("fixture catalogue offers no promotion action")
		}
		workflows := rankedPersonWorkflows(view.PersonWorkflows, view.WorkflowUses)

		rowActions, _, _ := personWorkflowActions(view, person, workflows)
		launcher := personWorkflowLauncherProps(view, person, PagePerson)

		rowHref := ""
		for _, action := range rowActions {
			if action.Href == want {
				rowHref = action.Href
			}
		}
		profileHref := ""
		for _, card := range launcher.Workflows {
			if card.Href == want {
				profileHref = card.Href
			}
		}
		if rowHref == "" {
			t.Fatal("directory exposes no promotion action for the eligible worker")
		}
		if profileHref == "" {
			t.Fatal("profile exposes no promotion action for the eligible worker")
		}
		if rowHref != profileHref {
			t.Fatalf("directory action = %q, profile action = %q, want the same promotion action", rowHref, profileHref)
		}
	})

	t.Run("terminal outcome stays visible in My Work and History", func(t *testing.T) {
		view := testView(PageWork)
		var terminal WorkItem
		for _, item := range view.Work {
			if item.Terminal {
				terminal = item
			}
		}
		if terminal.ID == "" {
			t.Fatal("fixture carries no terminal work item")
		}
		// The proposer's shape for their completed proposal: the server
		// names them INITIATOR with CLOSED responsibility at a terminal
		// stage (internal/intent/app journeyViewerProjection), so the
		// outcome they initiated stays in their collection.
		terminal.ViewerRelationships = []string{"INITIATOR"}
		terminal.ViewerResponsibility = "CLOSED"

		scoped := append([]WorkItem(nil), view.Work...)
		for i := range scoped {
			if scoped[i].ID == terminal.ID {
				scoped[i] = terminal
			}
		}
		mine := MyWorkItems(scoped, view.Viewer)
		seen := false
		for _, item := range mine {
			if item.ID == terminal.ID {
				seen = true
			}
		}
		if !seen {
			t.Errorf("My Work drops the initiated terminal item %s: completion is not visible in My Work", terminal.ID)
		}

		// The tracked tab is where My Work shows it: the initiated
		// terminal journey renders as a closed row with its detail link,
		// ranked below open work -- while the action queue stays
		// actionable-only and the badge counts open work alone.
		trackedView := testView(PageWork)
		trackedView.Work = scoped
		trackedView.WorkFilter = "tracked"
		trackedDoc, err := Render(trackedView)
		if err != nil {
			t.Fatal(err)
		}
		// Rows link through the work filter route (selected=<id>), not the
		// journey href: the tab keeps its own navigation state.
		if !strings.Contains(trackedDoc, "selected="+terminal.ID) {
			t.Errorf("My Work tracked tab omits terminal item %s: the completed journey is not visible in My Work", terminal.ID)
		}
		for _, item := range ActionableWorkItems(scoped) {
			if item.ID == terminal.ID {
				t.Errorf("My Work action queue carries terminal item %s: completed work competes with open attention", terminal.ID)
			}
		}

		history := CompletedHistory(scoped)
		found := false
		for _, entry := range history {
			if entry.ID == terminal.ID {
				found = true
				if entry.CompletedAt == "" {
					t.Errorf("history entry %s carries no completion evidence", entry.ID)
				}
			}
		}
		if !found {
			t.Errorf("completed-work history omits terminal item %s", terminal.ID)
		}

		for _, item := range ActionableWorkItems(scoped) {
			if item.ID == terminal.ID {
				t.Errorf("actionable badge counts terminal item %s", terminal.ID)
			}
		}
	})

	t.Run("no verdict never offers the promotion action", func(t *testing.T) {
		view := testView(PagePeople)
		unknown := Person{ID: "worker-unknown", Name: "Unknown Worker"}
		// The promotion verdict is per worker; other catalogue workflows
		// keep their own authority rules and are not this gate's business.
		promotionHref := ""
		for _, workflow := range view.PersonWorkflows {
			if workflow.ID == "promotion" && workflow.LaunchHref != nil {
				promotionHref = workflow.LaunchHref(unknown.ID)
			}
		}
		if promotionHref == "" {
			t.Fatal("fixture catalogue offers no promotion action")
		}
		workflows := rankedPersonWorkflows(view.PersonWorkflows, view.WorkflowUses)
		actions, reason, _ := personWorkflowActions(view, unknown, workflows)
		for _, action := range actions {
			if action.Href == promotionHref {
				t.Fatalf("worker with no verdict is offered the launchable promotion action %q", action.Href)
			}
		}
		if strings.TrimSpace(reason) == "" {
			t.Error("worker with no verdict leaves no reason behind")
		}
	})
}
