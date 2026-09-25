//go:build js && wasm

package main

import (
	"syscall/js"
	"testing"

	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func TestTodo_PM_030_ActionListenersRemainReadyWhenStartupRepeats(t *testing.T) {
	global := js.Global()
	previousDocument := global.Get("document")
	document := global.Get("Object").New()
	var addListener, removeListener js.Func
	registered := map[string]int{}
	addListener = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 2 {
			registered[args[0].String()]++
		}
		return nil
	})
	removeListener = js.FuncOf(func(_ js.Value, _ []js.Value) any { return nil })
	document.Set("addEventListener", addListener)
	document.Set("removeEventListener", removeListener)
	global.Set("document", document)
	t.Cleanup(func() {
		global.Set("document", previousDocument)
		if projectCreateListenerInstalled {
			document.Call("removeEventListener", "submit", projectCreateSubmit)
			projectCreateSubmit.Release()
		}
		if projectMoveListenerInstalled {
			document.Call("removeEventListener", "change", projectMoveChange)
			projectMoveChange.Release()
		}
		if projectBoardSettingsListenerInstalled {
			document.Call("removeEventListener", "submit", projectBoardSettingsSubmit)
			projectBoardSettingsSubmit.Release()
		}
		addListener.Release()
		removeListener.Release()
		projectCreateSubmit, projectMoveChange, projectBoardSettingsSubmit = js.Func{}, js.Func{}, js.Func{}
		projectCreateListenerInstalled, projectMoveListenerInstalled, projectBoardSettingsListenerInstalled = false, false, false
		projectCreateActionsReady = false
	})

	service := projectv1.NewProjectServiceClient(nil)
	cfg := journeyclient.Config{Tenant: "tenant", Subject: "subject"}
	navigate := func(string) {}
	if !bindProjectCreateForms(cfg, service, navigate, nil) || !bindProjectCreateForms(cfg, nil, nil, nil) {
		t.Fatal("repeated product startup cleared project create readiness")
	}
	if !bindProjectStatusMoves(cfg, service, nil) || !bindProjectStatusMoves(cfg, nil, nil) {
		t.Fatal("repeated product startup cleared task move readiness")
	}
	if !bindProjectBoardSettings(cfg, service, nil) || !bindProjectBoardSettings(cfg, nil, nil) {
		t.Fatal("repeated product startup cleared saved board settings readiness")
	}
	if registered["submit"] != 2 || registered["change"] != 1 {
		t.Fatalf("duplicate startup registered listeners again: %v", registered)
	}

}
