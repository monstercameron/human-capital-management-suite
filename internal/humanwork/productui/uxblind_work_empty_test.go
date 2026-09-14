package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestUXBLIND_012_EmptyWorkOmitsCircularActions(t *testing.T) {
	view := testView(PageWork)
	view.Work = nil
	view.SelectedWork = ""

	markup, err := ui.RenderToString(workPage(view))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, view.Locale.Text("work.action_queue_empty_title")) {
		t.Fatalf("empty work state missing: %s", markup)
	}
	if strings.Contains(markup, view.Locale.Text("work.view")) {
		t.Fatalf("empty My Work page contains a circular entry point: %s", markup)
	}
	if strings.Contains(markup, "work-preview") || strings.Contains(markup, view.Locale.Text("work.show_all")) {
		t.Fatal("empty inbox must not render a redundant selection panel")
	}
}

func TestUXBLIND008ScopeDoesNotPretendToBeADropdown(t *testing.T) {
	markup, err := ui.RenderToString(PageIdentityHeader(testView(PagePeople)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "⌄") || strings.Contains(markup, `<a class="scope"`) {
		t.Fatalf("scope must be informational, not a fake context switch: %s", markup)
	}
}

func TestUXBLIND_012_PopulatedWorkKeepsSelectionDetail(t *testing.T) {
	view := testView(PageWork)
	view.Viewer.PersonID = "worker-jordan"
	view.Work[0].AssigneeRef = "worker-jordan"
	markup, err := ui.RenderToString(workPage(view))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"work-preview",
		"Jordan Lee",
		"ENG2 G6 → ENG3 G7",
		view.Locale.Text("work.effective_date"),
		view.Locale.Text("work.open_journey"),
	} {
		if !strings.Contains(markup, expected) {
			t.Fatalf("populated work detail missing %q: %s", expected, markup)
		}
	}
}

func TestUXBLIND_012_HomeRetainsMyWorkEntryPoint(t *testing.T) {
	props := workCollectionProps(testView(PageHome), workCollectionOptions{Title: "Open promotion work"})
	if props.Footer.Action.Href == "" || props.Footer.Action.Label == "" {
		t.Fatalf("home work collection lost its My Work entry point: %+v", props.Footer.Action)
	}
}

func TestUXBlindHomePromotionStartsWithEmployeeChoice(t *testing.T) {
	markup, err := ui.RenderToString(homePage(testView(PageHome)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "Choose an employee to promote") || strings.Contains(markup, ">Terminal<") {
		t.Fatal("home must explain the first promotion step and use readable outcome labels")
	}
	if strings.Contains(markup, "Try another filter or return to all work.") {
		t.Fatal("empty work must not suggest a circular recovery")
	}
	if strings.Contains(markup, ">Choose a worker<") {
		t.Fatal("home must not duplicate the directory action beside promotion entry")
	}
}
