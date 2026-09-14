package productui

import (
	"strings"
	"testing"
)

func TestTodo_UXSCAN_005(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		`@media (min-width:1081px){.people-directory .data-table-scroll{max-height:none;min-height:0;overflow:visible;}`,
		`@media (min-width:1200px){.history-filter-controls{display:grid;grid-template-columns:`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("missing desktop scroll/filter contract %q", want)
		}
	}
}

func TestTodo_UXSCAN_005_Browser(t *testing.T) {
	for _, page := range []PageID{PagePeople, PageHistory} {
		view := NewView(page, "HarborCare", "viewer", "scope")
		if page == PagePeople {
			view.People = []Person{{ID: "worker-1", Name: "Ari", WorkerNumber: "HC-1"}}
		}
		markup, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if page == PagePeople && (!strings.Contains(markup, `id="people-directory-table-viewport"`) || !strings.Contains(markup, `main-scroll`)) {
			t.Fatal("People lost its shared table and page scroll regions")
		}
		if page == PageHistory && (!strings.Contains(markup, `class="history-filter-controls"`) || !strings.Contains(markup, `id="history-search"`) || !strings.Contains(markup, `placeholder="Search history"`)) {
			t.Fatal("History lost its shared filter controls")
		}
	}
}

func TestTodo_UXSCAN_005_Accessibility(t *testing.T) {
	view := NewView(PagePeople, "HarborCare", "viewer", "scope")
	view.People = []Person{{ID: "worker-1", Name: "Ari", WorkerNumber: "HC-1"}}
	markup, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `aria-label="Authorized people"`) || !strings.Contains(markup, `tabIndex="0"`) {
		t.Fatal("table viewport is not named and keyboard reachable")
	}
}

func TestTodo_UXSCAN_005_Regression(t *testing.T) {
	css := Stylesheet()
	if !strings.Contains(css, `@media (max-width:760px){.history-filter-controls{grid-template-columns:1fr;}`) {
		t.Fatal("narrow History filters lost their single-column layout")
	}
}
