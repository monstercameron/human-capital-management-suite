package productui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// UXLIVE-033's RED was measured on the live People directory: most rows
// ended in a prominent "Unavailable" control, a worker with an active
// promotion did not make "Open active promotion" the obvious continuation,
// and an eligible worker with one authorized workflow still needed a menu.

// uxlive033View is a directory of four workers in four different states,
// with only the promotion workflow in the catalogue.
func uxlive033View() View {
	view := testView(PagePeople)
	view.PersonWorkflows = view.PersonWorkflows[:1]
	view.People = []Person{
		{ID: "worker-jordan", Name: "Jordan Lee", PromotionAvailability: PromotionActiveConflict},
		{ID: "worker-avery", Name: "Avery Patel", PromotionAvailability: PromotionEligible},
		{ID: "worker-ines", Name: "Ines Moreau", PromotionAvailability: PromotionIneligible},
		{ID: "worker-kai", Name: "Kai Tanaka", PromotionAvailability: PromotionActiveConflict},
	}
	// Jordan's open request is in the viewer's work; Kai's is not.
	view.Work = view.Work[:1]
	return view
}

func uxlive033Rows(t *testing.T, view View) map[string]PeopleRowProps {
	t.Helper()
	rows := peopleRowProps(view, peoplePageWindow{People: view.People, Page: 1})
	byID := make(map[string]PeopleRowProps, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	return byID
}

func uxlive033RowMarkup(t *testing.T, row PeopleRowProps) string {
	t.Helper()
	markup, err := ui.RenderToString(ui.CreateElement(PeopleRow, row))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

var uxlive033ActionCell = regexp.MustCompile(`(?s)<td class="data-table-cell people-row-actions[^"]*"[^>]*>(.*)</td>`)

// uxlive033Controls counts the interactive controls in a row's action
// cell: links, buttons and disclosure triggers outside a popover panel.
func uxlive033Controls(t *testing.T, markup string) int {
	t.Helper()
	cell := uxlive033ActionCell.FindStringSubmatch(markup)
	if cell == nil {
		t.Fatalf("no action cell:\n%s", markup)
	}
	top := regexp.MustCompile(`(?s)<div class="popover-surface.*?</div>`).ReplaceAllString(cell[1], "")
	return strings.Count(top, "<a ") + strings.Count(top, "<button") + strings.Count(top, "<summary")
}

// TestTodo_UXLIVE_033 is the primary red/green test: every row has exactly
// one control. An active request is a direct "Open promotion" to that
// journey, one eligible workflow is a direct "Start promotion", several
// share the menu, and nothing startable is quiet text with an info button.
func TestTodo_UXLIVE_033(t *testing.T) {
	view := uxlive033View()
	rows := uxlive033Rows(t, view)

	jordan := uxlive033RowMarkup(t, rows["worker-jordan"])
	if PersonWorkflowPresentationFor(rows["worker-jordan"].QuickActions) != PersonWorkflowContinue ||
		!strings.Contains(jordan, `class="button secondary people-row-action people-row-direct people-row-continue" href="/workspace/app/journeys?journey=intent-1"`) ||
		!strings.Contains(jordan, ">Open promotion</a>") {
		t.Fatalf("an active request is not a direct continuation to that journey:\n%s", jordan)
	}

	avery := uxlive033RowMarkup(t, rows["worker-avery"])
	if PersonWorkflowPresentationFor(rows["worker-avery"].QuickActions) != PersonWorkflowDirect ||
		!strings.Contains(avery, `href="/workspace/app/journeys?mode=new&amp;worker=worker-avery"`) || !strings.Contains(avery, ">Start promotion</a>") {
		t.Fatalf("one eligible workflow is not a direct Start promotion:\n%s", avery)
	}

	ines := uxlive033RowMarkup(t, rows["worker-ines"])
	if !strings.Contains(ines, `<span class="people-availability-note">No workflow to start</span>`) ||
		!strings.Contains(ines, `class="people-reason-trigger"`) || strings.Contains(ines, "people-workflow-chevron") ||
		!strings.Contains(ines, PromotionAvailabilityReason(view.Locale, PromotionIneligible)) || strings.Contains(ines, ">Unavailable<") {
		t.Fatalf("an ineligible row is not quiet text with an info button:\n%s", ines)
	}

	several := testView(PagePeople)
	menuRows := uxlive033Rows(t, several)
	if PersonWorkflowPresentationFor(menuRows["worker-avery"].QuickActions) != PersonWorkflowMenu {
		t.Fatalf("two workflows are not a menu: %+v", menuRows["worker-avery"].QuickActions)
	}
	if markup := uxlive033RowMarkup(t, menuRows["worker-avery"]); !strings.Contains(markup, `aria-label="Choose a workflow for Avery Patel · NW-40118"`) {
		t.Fatalf("the shared menu is gone:\n%s", markup)
	}

	for _, rowSet := range []map[string]PeopleRowProps{rows, menuRows} {
		for id, row := range rowSet {
			if got := uxlive033Controls(t, uxlive033RowMarkup(t, row)); got != 1 {
				t.Fatalf("%s has %d controls in its action cell, want exactly one:\n%s", id, got, uxlive033RowMarkup(t, row))
			}
		}
	}
}

// TestTodo_UXLIVE_033_Integration proves one projection feeds the People
// row, the person page's launcher and the shell's action launcher: all three
// send the same person to the same continuation or start address.
func TestTodo_UXLIVE_033_Integration(t *testing.T) {
	view := uxlive033View()
	rows := uxlive033Rows(t, view)
	launcher := personActionLauncherItems(view)
	for _, person := range view.People {
		projection := ResolvePersonWorkflowActions(view, person)
		row := rows[person.ID]
		if len(row.QuickActions) != len(projection.Actions) || row.WorkflowsUnavailableReason != projection.Reason {
			t.Fatalf("%s: the row and the projection disagree: row=%+v projection=%+v", person.ID, row, projection)
		}
		for index, action := range projection.Actions {
			if row.QuickActions[index].Href != action.Href {
				t.Fatalf("%s: row action %d goes to %q, projection to %q", person.ID, index, row.QuickActions[index].Href, action.Href)
			}
			found := false
			for _, item := range launcher {
				found = found || item.Href == action.Href
			}
			if !found {
				t.Fatalf("%s: the action launcher does not offer %q", person.ID, action.Href)
			}
		}
	}
	jordan := view.People[0]
	cards := personWorkflowLauncherProps(view, jordan, PagePerson).Workflows
	if len(cards) != 1 || cards[0].Href != rows["worker-jordan"].QuickActions[0].Href {
		t.Fatalf("the person launcher does not continue the same request: %+v", cards)
	}
	ines := view.People[2]
	if cards := personWorkflowLauncherProps(view, ines, PagePerson).Workflows; len(cards) != 0 {
		t.Fatalf("the person launcher offers a start the row withholds: %+v", cards)
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "people-row-continue") || !strings.Contains(doc, "people-reason-trigger") {
		t.Fatal("the served People page does not render the projected states")
	}
	// A saved My Work tab must not narrow the population the People row
	// resolves continuations from: live, every active request fell back to
	// an explanation because Work had been narrowed to one saved tab.
	filtered := ApplyRequest(view, PageRequest{Page: PagePeople, WorkFilter: "tracked"})
	if projection := ResolvePersonWorkflowActions(filtered, filtered.People[0]); !projection.ActiveRequest {
		t.Fatal("a work tab on the People route hid the person's active request")
	}
}

// TestTodo_UXLIVE_033_Browser keeps the unavailable state quiet in the
// served stylesheet and every action cell one control high.
func TestTodo_UXLIVE_033_Browser(t *testing.T) {
	css := Stylesheet()
	rule := regexp.MustCompile(`\.people-unavailable-menu>summary\.people-reason-trigger\{[^}]*\}`).FindString(css)
	for _, want := range []string{"background:transparent", "border:0", "border-radius:50%"} {
		if !strings.Contains(rule, want) {
			t.Fatalf("the reason trigger is not a quiet icon button (%s missing): %q", want, rule)
		}
	}
	note := regexp.MustCompile(`\.people-availability-note\{[^}]*\}`).FindString(css)
	if !strings.Contains(note, "color:var(--muted)") {
		t.Fatalf("the unavailable text is not muted: %q", note)
	}
	if !strings.Contains(css, ".people-availability{align-items:center;display:inline-flex;gap:4px;min-height:40px;}") {
		t.Fatal("the unavailable cell is not one control high")
	}
}

// TestTodo_UXLIVE_033_Accessibility names every direct action after the
// person and gives the info button the reason as its accessible name.
func TestTodo_UXLIVE_033_Accessibility(t *testing.T) {
	view := uxlive033View()
	rows := uxlive033Rows(t, view)
	for id, want := range map[string]string{
		"worker-jordan": `aria-label="Open the active promotion for Jordan Lee"`,
		"worker-avery":  `aria-label="Start Promotion for Avery Patel"`,
		"worker-ines":   `aria-label="Why workflows are unavailable for Ines Moreau: `,
	} {
		if markup := uxlive033RowMarkup(t, rows[id]); !strings.Contains(markup, want) {
			t.Fatalf("%s is missing %s:\n%s", id, want, markup)
		}
	}
	if markup := uxlive033RowMarkup(t, rows["worker-ines"]); !strings.Contains(markup, `aria-describedby="people-unavailable-worker-ines"`) {
		t.Fatalf("the info button is not tied to its reason:\n%s", markup)
	}
}

// TestTodo_UXLIVE_033_Security gives a viewer without authority the same
// non-disclosing shape and wording for every worker, whatever the worker's
// hidden eligibility or open request: no continuation, one withheld reason.
func TestTodo_UXLIVE_033_Security(t *testing.T) {
	view := uxlive033View()
	view.EffectivePermissions = []RolePagePermission{{Page: PagePeople, View: true}, {Page: PageJourneys, View: true}}
	rows := uxlive033Rows(t, view)
	withheld := PromotionAvailabilityReason(view.Locale, PromotionWithheld)
	var shape string
	for _, person := range view.People {
		row := rows[person.ID]
		if len(row.QuickActions) != 0 || row.WorkflowsUnavailableReason != withheld {
			t.Fatalf("%s: an unauthorized viewer sees %+v", person.ID, row)
		}
		cell := uxlive033ActionCell.FindStringSubmatch(uxlive033RowMarkup(t, row))
		if cell == nil {
			t.Fatalf("%s: no action cell", person.ID)
		}
		normalized := strings.NewReplacer(person.ID, "ID", person.Name, "NAME").Replace(cell[1])
		if shape == "" {
			shape = normalized
			continue
		}
		if normalized != shape {
			t.Fatalf("%s: the unauthorized shape differs by hidden state:\n%s\nvs\n%s", person.ID, normalized, shape)
		}
	}
	for _, item := range personActionLauncherItems(view) {
		if item.Href != "" || item.Reason != withheld {
			t.Fatalf("the action launcher discloses a per-person state to an unauthorized viewer: %+v", item)
		}
	}
}

// TestTodo_UXLIVE_033_Regression keeps the pieces that were right: a row
// with no catalogue still explains itself generically, an ineligible worker
// is never offered Start, and a continuation is the row's one control even
// when other workflows exist (they stay on the person page).
func TestTodo_UXLIVE_033_Regression(t *testing.T) {
	view := testView(PagePeople)
	rows := uxlive033Rows(t, view)
	jordan := uxlive033RowMarkup(t, rows["worker-jordan"])
	if !strings.Contains(jordan, "people-row-continue") || strings.Contains(jordan, `name="people-workflows"`) {
		t.Fatalf("a continuation beside another workflow is not the row's one control:\n%s", jordan)
	}
	empty := view
	empty.PersonWorkflows = nil
	markup := uxlive033RowMarkup(t, uxlive033Rows(t, empty)["worker-avery"])
	if !strings.Contains(markup, `aria-label="`+view.Locale.Text("people.workflow_unavailable_generic_aria", map[string]string{"name": "Avery Patel · NW-40118"})) {
		t.Fatalf("a row with no catalogue lost its generic explanation:\n%s", markup)
	}
	ineligible := uxlive033View()
	if projection := ResolvePersonWorkflowActions(ineligible, ineligible.People[2]); len(projection.Actions) != 0 {
		t.Fatalf("an ineligible worker is offered %+v", projection.Actions)
	}
}
