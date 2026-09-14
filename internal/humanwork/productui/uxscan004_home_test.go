package productui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestTodo_UXSCAN_004 proves the all-empty composition has one next step and
// one compact continuity summary instead of repeating empty cards.
func TestTodo_UXSCAN_004(t *testing.T) {
	markup, err := ui.RenderToString(homePage(NewView(PageHome, "HarborCare", "viewer", "scope")))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `home-grid-empty`) || !strings.Contains(markup, `home-continuity-summary`) {
		t.Fatalf("empty Home did not use compact composition: %s", markup)
	}
	if strings.Contains(markup, "Needs your attention") || strings.Contains(markup, "Tracked requests") || strings.Contains(markup, "Recent people") {
		t.Fatalf("empty Home repeated populated-state cards: %s", markup)
	}
}

func TestTodo_UXSCAN_004_Browser(t *testing.T) {
	view := NewView(PageHome, "HarborCare", "viewer", "scope")
	markup, err := ui.RenderToString(homePage(view))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(markup, `class="surface panel home-continuity-summary"`) != 1 {
		t.Fatalf("empty Home should have one continuity summary: %s", markup)
	}
}

func TestTodo_UXSCAN_004_Accessibility(t *testing.T) {
	view := NewView(PageHome, "HarborCare", "viewer", "scope")
	markup, err := ui.RenderToString(homePage(view))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "Choose an employee to promote") || !strings.Contains(markup, "<a ") {
		t.Fatalf("empty Home omitted the authorized next step: %s", markup)
	}
}

func TestTodo_UXSCAN_004_Regression(t *testing.T) {
	props := HomePageProps{
		ShowWork: true, Work: WorkCollectionProps{Title: "Needs action", Rows: []WorkRowProps{{Title: "Approval"}}},
		ShowDrafts: true, Drafts: WorkCollectionProps{Title: "Resumable drafts", Rows: []WorkRowProps{{Title: "Draft"}}},
		QuickStart: QuickActionsProps{Title: "Start", Actions: []ActionLinkProps{{Label: "Open", Href: "/open"}}},
	}
	markup, err := ui.RenderToString(HomePage(props))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "home-grid-empty") || !strings.Contains(markup, "Approval") || !strings.Contains(markup, "Draft") {
		t.Fatalf("populated Home did not restore distinct cards: %s", markup)
	}
}

func TestTodo_UXSCAN_004_Performance(t *testing.T) {
	view := NewView(PageHome, "HarborCare", "viewer", "scope")
	for index := 0; index < 100; index++ {
		view.People = append(view.People, Person{ID: fmt.Sprintf("worker-%03d", index), Name: "Visible worker"})
	}
	markup, err := ui.RenderToString(homePage(view))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "home-grid-empty") || len(markup) > 6000 {
		t.Fatalf("quiet Home grew with unrelated directory records: bytes=%d", len(markup))
	}
}
