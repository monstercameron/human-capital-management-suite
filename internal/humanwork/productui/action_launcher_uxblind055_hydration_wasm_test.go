//go:build js && wasm

package productui

import (
	"strings"
	"syscall/js"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/testkit/render"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_055_WorkflowDesignerHydrationKeepsLauncherName(t *testing.T) {
	view := testView(PageWorkflowDesigner)
	serverProps := actionLauncherProps(view)
	if !actionLauncherHasAction(serverProps.Items) {
		t.Fatal("server projection does not exercise action mode")
	}
	fixture := render.New(t)
	fixture.Render(ui.CreateElement(ActionLauncher, serverProps))
	assertUXBlind055LauncherName(t, fixture)

	// The server may have an action projection before the client replaces it
	// with the destination-only first hydration projection. The route still
	// determines the launcher's name during that update.
	clientProps := serverProps
	clientProps.Items = []ActionLauncherItem{{
		Page: PagePeople, Kind: ActionLauncherDestination, Action: "view", Availability: ActionState{Availability: ActionAvailable},
		ID: "destination:people", Label: "People", Href: "/workspace/app/people", IsNavigationDestination: true,
	}}
	if actionLauncherHasAction(clientProps.Items) {
		t.Fatal("hydration projection still contains an executable action")
	}
	fixture.Rerender(ui.CreateElement(ActionLauncher, clientProps))
	assertUXBlind055LauncherName(t, fixture)
}

func TestTodo_UXBLIND_055_JourneysShellTriggerOpensLauncher(t *testing.T) {
	global := js.Global()
	previousDocument := global.Get("document")
	document := global.Get("Object").New()
	lookup := js.FuncOf(func(js.Value, []js.Value) any { return js.Null() })
	document.Set("getElementById", lookup)
	global.Set("document", document)
	t.Cleanup(func() {
		global.Set("document", previousDocument)
		lookup.Release()
	})

	view := testView(PageJourneys)
	fixture := render.New(t)
	fixture.Render(ui.CreateElement(func(struct{}) ui.Node {
		return BuildShell(view, html.Div(html.Props{}), true)
	}, struct{}{}))
	trigger := fixture.ByID("action-launcher-trigger")
	if trigger == nil || trigger.Attr("aria-expanded") != "false" {
		t.Fatal("Journeys shell did not start with a closed action launcher")
	}

	fixture.ClickByID("action-launcher-trigger")
	trigger = fixture.ByID("action-launcher-trigger")
	dialog := fixture.ByID("action-launcher-dialog")
	if trigger == nil || trigger.Attr("aria-expanded") != "true" {
		t.Fatalf("trigger click left the launcher closed: expanded=%q", trigger.Attr("aria-expanded"))
	}
	if dialog == nil || strings.Contains(dialog.Attr("class"), "action-launcher-dialog-hidden") || dialog.Attr("aria-hidden") == "true" {
		t.Fatalf("trigger click did not show the action launcher dialog: class=%q aria-hidden=%q", dialog.Attr("class"), dialog.Attr("aria-hidden"))
	}
}

func assertUXBlind055LauncherName(t *testing.T, fixture *render.Fixture) {
	t.Helper()
	trigger := fixture.ByID("action-launcher-trigger")
	dialog := fixture.ByID("action-launcher-dialog")
	if trigger == nil || dialog == nil {
		t.Fatal("hydrated launcher is missing its trigger or dialog")
	}
	want := "Start an action"
	if got := trigger.Text(); got != want {
		t.Fatalf("visible Workflow Designer launcher label = %q, want %q", got, want)
	}
	for _, got := range []string{trigger.Attr("aria-label"), trigger.Attr("title"), dialog.Attr("aria-label")} {
		if got != want && got != want+" (Alt+K)" {
			t.Fatalf("Workflow Designer launcher name = %q, want %q with an optional shortcut suffix", got, want)
		}
	}
}
