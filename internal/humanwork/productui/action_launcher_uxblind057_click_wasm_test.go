//go:build js && wasm

package productui

import (
	"strings"
	"syscall/js"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/testkit/render"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestActionLauncherTriggerClickUpdatesOpenState(t *testing.T) {
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

	fixture := render.New(t)
	fixture.Render(ui.CreateElement(ActionLauncher, ActionLauncherProps{
		Items: []ActionLauncherItem{{
			ID: "start:people", Label: "People", Href: "/workspace/app/people",
			IsNavigationDestination: true,
		}},
	}))

	root := fixture.ByID("action-launcher")
	trigger := fixture.ByID("action-launcher-trigger")
	dialog := fixture.ByID("action-launcher-dialog")
	if root == nil || trigger == nil || dialog == nil {
		t.Fatal("rendered launcher is missing its root, trigger, or dialog")
	}
	if got := trigger.Attr("aria-expanded"); got != "false" {
		t.Fatalf("initial aria-expanded = %q, want false", got)
	}
	if got := root.Attr("class"); got != "action-launcher" {
		t.Fatalf("initial launcher class = %q, want closed class", got)
	}

	fixture.ClickByID("action-launcher-trigger")
	trigger = fixture.ByID("action-launcher-trigger")
	root = fixture.ByID("action-launcher")
	dialog = fixture.ByID("action-launcher-dialog")
	if got := trigger.Attr("aria-expanded"); got != "true" {
		t.Fatalf("after one click aria-expanded = %q, want true", got)
	}
	if got := root.Attr("class"); got != "action-launcher action-launcher-open" {
		t.Fatalf("after one click launcher class = %q, want open class", got)
	}
	if got := dialog.Attr("class"); strings.Contains(got, "action-launcher-dialog-hidden") {
		t.Fatalf("dialog retains its closed-state class after one click: %q", got)
	}
	if got := dialog.Attr("aria-hidden"); got == "true" {
		t.Fatal("dialog remains aria-hidden after one click")
	}
}
