package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// UXLIVE-023's RED was measured on the running server: Jane's profile showed
// "No matching workflows / Try a workflow name, category, or outcome."
// directly above an "Active workflows" list containing her in-progress
// promotion. Nothing had been searched -- the filter box was empty -- so the
// launcher answered a question the reader had not asked, and answered it in
// a way that contradicted the list underneath.

func uxlive023Launcher(t *testing.T, query string, total int) string {
	t.Helper()
	locale := ResolveProductLocale("en-US")
	doc, err := ui.RenderToString(WorkflowLauncher(WorkflowLauncherProps{
		I18nProps:  I18nProps{Locale: locale},
		PersonName: "Jane",
		TotalCount: total,
		Filter:     WorkflowFilterProps{Query: query, Action: "/workspace/app/person"},
	}))
	if err != nil {
		t.Fatalf("render launcher: %v", err)
	}
	return doc
}

// TestTodo_UXLIVE_023 is the primary red/green test: an unsearched launcher
// never reports that a search found nothing.
func TestTodo_UXLIVE_023(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	noMatch, noMatchDetail := locale.Text("workflow.none"), locale.Text("workflow.none_detail")

	unsearched := uxlive023Launcher(t, "", 2)
	if strings.Contains(unsearched, noMatch) || strings.Contains(unsearched, noMatchDetail) {
		t.Fatalf("an empty filter box still reports that a search matched nothing:\n%s", unsearched)
	}
	if !strings.Contains(unsearched, locale.Text("workflow.unavailable")) {
		t.Fatalf("an unsearched launcher with nothing to start says nothing at all:\n%s", unsearched)
	}

	// A real search that matches nothing still says so, and says how to
	// recover.
	searched := uxlive023Launcher(t, "payroll", 2)
	if !strings.Contains(searched, noMatch) || !strings.Contains(searched, noMatchDetail) {
		t.Fatalf("a search that matched nothing no longer says so:\n%s", searched)
	}
}

// TestTodo_UXLIVE_023_Browser keeps the person object's identity to one
// statement: the heading. The breadcrumb names where the reader is, and the
// profile card carries facts, not a third copy of the name.
func TestTodo_UXLIVE_023_Browser(t *testing.T) {
	view := promoux012View(PagePerson)
	doc, err := ui.RenderToString(personPage(view))
	if err != nil {
		t.Fatalf("render person: %v", err)
	}
	name := ""
	for _, candidate := range view.People {
		if candidate.ID == view.SelectedPerson {
			name = candidate.Name
		}
	}
	if name == "" {
		t.Fatalf("fixture has no selected person")
	}
	if got := strings.Count(doc, ">"+name+"<"); got > 2 {
		t.Fatalf("the person's name is stated %d times before any fact:\n%s", got, doc)
	}
}

// TestTodo_UXLIVE_023_Security proves the change cannot widen disclosure:
// the unsearched state says only that nothing is available to start, never
// why, and never names a workflow the viewer may not start.
func TestTodo_UXLIVE_023_Security(t *testing.T) {
	doc := uxlive023Launcher(t, "", 5)
	for _, forbidden := range []string{"Promotion", "promotion", "in progress"} {
		if strings.Contains(doc, forbidden) {
			t.Fatalf("the unsearched empty state discloses %q:\n%s", forbidden, doc)
		}
	}
}
