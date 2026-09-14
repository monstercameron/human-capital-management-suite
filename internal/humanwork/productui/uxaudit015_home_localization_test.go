package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXAUDIT_015_TrackedStatusUsesServerLocalization(t *testing.T) {
	view := NewView(PageHome, "HarborCare", "viewer", "scope")
	view.Viewer = ViewerProfile{PersonID: "viewer"}
	view = ApplyLocale(view, ResolveProductLocale("de-DE"))
	view = ApplyPagePermissions(view, []RolePagePermission{{Page: PageWork, View: true}})
	view.Work = []WorkItem{{
		ID: "request", AssigneeRef: "viewer", Title: "Benefits request",
		Status: "Waiting for effective date", StatusKey: "journey.stage_waiting_effective",
	}}

	markup, err := ui.RenderToString(homePage(view))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "Wartet auf Wirksamkeitsdatum") {
		t.Fatalf("tracked request did not use localized status projection: %s", markup)
	}
	if strings.Contains(markup, "Waiting for effective date") {
		t.Fatalf("tracked request leaked raw status label: %s", markup)
	}
}
