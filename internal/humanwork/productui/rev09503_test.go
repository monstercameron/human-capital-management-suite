package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// REV-095-03: the person page counted the launcher's cards after the search
// had filtered them, so a search that matched nothing looked exactly like a
// launcher with no card at all and fell through to the generic "not
// available yet" sentence. The launcher now counts what it holds before the
// search, so an empty search result gets the launcher's own no-match copy.

func rev09503Launcher(t *testing.T, view View) string {
	t.Helper()
	person, ok := exactPerson(view)
	if !ok {
		t.Fatalf("fixture has no selected person")
	}
	props := personWorkflowLauncherProps(view, person, PagePerson)
	props.I18nProps = I18nProps{Locale: view.Locale} // as PersonProfile passes it down
	doc, err := ui.RenderToString(WorkflowLauncher(props))
	if err != nil {
		t.Fatalf("render launcher: %v", err)
	}
	return doc
}

// TestTodo_REV_095_03 is the primary: a launcher that holds cards but whose
// search matches none of them says the search found nothing and how to
// broaden it; the generic unavailable sentence is reserved for no card.
func TestTodo_REV_095_03(t *testing.T) {
	for _, tag := range []string{"en-US", "de-DE", "ar"} {
		t.Run(tag, func(t *testing.T) {
			locale := ResolveProductLocale(tag)
			generic := ordinaryUnavailableCopy(locale.Resolved)

			searched := testView(PagePerson)
			searched.Locale = locale
			searched.WorkflowQuery = "payroll-adjustment-that-matches-nothing"
			person, _ := exactPerson(searched)
			props := personWorkflowLauncherProps(searched, person, PagePerson)
			if len(props.Workflows) != 0 {
				t.Fatalf("fixture search matched %d cards; want none", len(props.Workflows))
			}
			if props.TotalCount != 2 {
				t.Fatalf("launcher reports %d cards held; want the 2 it holds before the search", props.TotalCount)
			}
			doc := rev09503Launcher(t, searched)
			if !strings.Contains(doc, locale.Text("workflow.none")) || !strings.Contains(doc, locale.Text("workflow.none_detail")) {
				t.Fatalf("an empty search result lacks the launcher's own no-match copy:\n%s", doc)
			}
			if strings.Contains(doc, generic) || strings.Contains(doc, locale.Text("workflow.unavailable")) {
				t.Fatalf("an empty search result still renders the no-card sentence:\n%s", doc)
			}
			// The filter stays on screen so the search can be broadened.
			if !strings.Contains(doc, `id="workflow-search"`) {
				t.Fatalf("the filter disappeared after a search that matched nothing:\n%s", doc)
			}

			none := testView(PagePerson)
			none.Locale = locale
			none.PersonWorkflows = nil
			doc = rev09503Launcher(t, none)
			if !strings.Contains(doc, locale.Text("workflow.unavailable")) {
				t.Fatalf("a launcher with no card lost its unavailable state:\n%s", doc)
			}
			if strings.Contains(doc, locale.Text("workflow.none_detail")) {
				t.Fatalf("a launcher with no card claims a search matched nothing:\n%s", doc)
			}
		})
	}
}

// TestTodo_REV_095_03_Browser renders the whole served person page for a
// search that matches nothing and asserts the document carries the
// launcher's no-match state inside its result area, not the generic one.
func TestTodo_REV_095_03_Browser(t *testing.T) {
	view := testView(PagePerson)
	view.WorkflowQuery = "zzz-no-such-workflow"
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	locale := ResolveProductLocale("en-US")
	results := strings.Index(doc, `class="workflow-results"`)
	if results < 0 {
		t.Fatalf("person page has no workflow result area:\n%s", doc)
	}
	tail := doc[results:]
	if !strings.Contains(tail, locale.Text("workflow.none")) {
		t.Fatalf("the served result area does not say the search matched nothing")
	}
	if strings.Contains(tail[:min(len(tail), 600)], ordinaryUnavailableCopy(DefaultProductLocale)) {
		t.Fatalf("the served result area renders the generic unavailable sentence")
	}
	// The live defect: a worker whose only card is the resume link for an
	// open promotion. The composition moves that card to the active list,
	// and the launcher it leaves behind explains itself with that card's own
	// reason instead of the generic sentence.
	active := promoux012View(PagePerson)
	active.PersonWorkflows = active.PersonWorkflows[:1] // promotion only
	activeDoc, err := Render(active)
	if err != nil {
		t.Fatal(err)
	}
	at := strings.Index(activeDoc, `class="workflow-results"`)
	if at < 0 {
		t.Fatalf("active-promotion profile has no workflow result area")
	}
	area := activeDoc[at:min(len(activeDoc), at+600)]
	if strings.Contains(area, ordinaryUnavailableCopy(DefaultProductLocale)) {
		t.Fatalf("a launcher whose card moved to the active list renders the generic sentence: %s", area)
	}
	if !strings.Contains(area, PromotionAvailabilityReason(locale, PromotionActiveConflict)) {
		t.Fatalf("a launcher whose card moved to the active list does not give that card's reason: %s", area)
	}
	// A search the matched set supports still renders the matching card.
	view.WorkflowQuery = "transfer"
	doc, err = Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "Internal transfer") || strings.Contains(doc, locale.Text("workflow.none_detail")) {
		t.Fatalf("a matching search no longer lists its card")
	}
}

// TestTodo_REV_095_03_Security proves counting the unfiltered cards does not
// widen what a viewer without start authority is told: an unauthorized
// viewer's launcher still holds nothing and names no workflow, with or
// without a search.
func TestTodo_REV_095_03_Security(t *testing.T) {
	for _, query := range []string{"", "promotion"} {
		view := testView(PagePerson)
		view.EffectivePermissions = []RolePagePermission{{Page: PagePeople, View: true}, {Page: PageJourneys, View: true}}
		view.WorkflowQuery = query
		person, _ := exactPerson(view)
		props := personWorkflowLauncherProps(view, person, PagePerson)
		if props.TotalCount != 0 || len(props.Workflows) != 0 {
			t.Fatalf("query %q: an unauthorized viewer's launcher holds %d cards (%d shown)", query, props.TotalCount, len(props.Workflows))
		}
		doc := rev09503Launcher(t, view)
		for _, forbidden := range []string{"Internal transfer", "Career mobility", ResolveProductLocale("en-US").Text("workflow.none_detail")} {
			if strings.Contains(doc, forbidden) {
				t.Fatalf("query %q: unauthorized launcher discloses %q:\n%s", query, forbidden, doc)
			}
		}
	}
}
