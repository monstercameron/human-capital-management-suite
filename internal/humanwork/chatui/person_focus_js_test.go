//go:build js && wasm

package chatui

import (
	"syscall/js"
	"testing"
)

func TestPersonFocusRestoresTriggerAndMovesToComposer(t *testing.T) {
	global := js.Global()
	personPaneWasOpen = false
	oldDocument, oldFrame := global.Get("document"), global.Get("requestAnimationFrame")
	defer func() {
		global.Set("document", oldDocument)
		global.Set("requestAnimationFrame", oldFrame)
		personPaneWasOpen = false
		personTrigger = js.Undefined()
		personFocusComposer = false
	}()
	var focused []string
	focus := func(name string) js.Func {
		return js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) != 1 || !args[0].Get("preventScroll").Bool() {
				t.Errorf("%s focus moved scroll", name)
			}
			focused = append(focused, name)
			return nil
		})
	}
	headingFocus, triggerFocus, composerFocus := focus("heading"), focus("trigger"), focus("composer")
	defer headingFocus.Release()
	defer triggerFocus.Release()
	defer composerFocus.Release()
	heading := js.ValueOf(map[string]any{"isConnected": true, "focus": headingFocus})
	composer := js.ValueOf(map[string]any{"isConnected": true, "disabled": false, "focus": composerFocus})
	personTrigger = js.ValueOf(map[string]any{"isConnected": true, "focus": triggerFocus})
	headingQueries := 0
	query := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if args[0].String() == ".chat-workspace .person-pane-heading" {
			headingQueries++
			if headingQueries < 3 {
				return js.Undefined()
			}
			return heading
		}
		return composer
	})
	defer query.Release()
	global.Set("document", js.ValueOf(map[string]any{"querySelector": query}))
	frame := js.FuncOf(func(_ js.Value, args []js.Value) any { args[0].Invoke(); return nil })
	defer frame.Release()
	global.Set("requestAnimationFrame", frame)
	syncPersonFocus(true)
	syncPersonFocus(false)
	if headingQueries != 3 {
		t.Fatalf("heading was not retried after render: %d queries", headingQueries)
	}
	if len(focused) != 2 || focused[0] != "heading" || focused[1] != "trigger" {
		t.Fatalf("focus order = %v", focused)
	}
	FocusComposer()
	if len(focused) != 3 || focused[2] != "composer" {
		t.Fatalf("composer focus = %v", focused)
	}
}

func TestFocusComposerForWaitsForSelectedRoom(t *testing.T) {
	global := js.Global()
	oldDocument, oldFrame := global.Get("document"), global.Get("requestAnimationFrame")
	defer func() {
		global.Set("document", oldDocument)
		global.Set("requestAnimationFrame", oldFrame)
		personFocusComposer = false
		personPaneWasOpen = false
		personTrigger = js.Undefined()
	}()
	selected := "old"
	paneOpen := true
	focused := 0
	composerFocus := js.FuncOf(func(_ js.Value, args []js.Value) any {
		focused++
		if !args[0].Get("preventScroll").Bool() {
			t.Error("composer focus scrolled the page")
		}
		return nil
	})
	defer composerFocus.Release()
	composer := js.ValueOf(map[string]any{"disabled": false, "focus": composerFocus})
	roomQuery := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if args[0].String() == ".person-pane" {
			if paneOpen {
				return js.ValueOf(map[string]any{"isConnected": true})
			}
			return js.Null()
		}
		return composer
	})
	defer roomQuery.Release()
	root := js.ValueOf(map[string]any{"isConnected": true, "dataset": map[string]any{"selectedId": "old"}, "querySelector": roomQuery})
	query := js.FuncOf(func(js.Value, []js.Value) any { return root })
	defer query.Release()
	global.Set("document", js.ValueOf(map[string]any{"querySelector": query}))
	var pending []js.Value
	frame := js.FuncOf(func(_ js.Value, args []js.Value) any { pending = append(pending, args[0]); return nil })
	defer frame.Release()
	global.Set("requestAnimationFrame", frame)
	runFrame := func() {
		t.Helper()
		if len(pending) == 0 {
			t.Fatal("no animation frame pending")
		}
		next := pending[0]
		pending = pending[1:]
		next.Invoke()
	}
	FocusComposerFor("new")
	if len(pending) != 1 || focused != 0 {
		t.Fatalf("focused before room selection: %d", focused)
	}
	runFrame()
	if focused != 0 {
		t.Fatal("focused the previous room")
	}
	selected = "new"
	root.Get("dataset").Set("selectedId", selected)
	runFrame()
	if focused != 0 {
		t.Fatal("focused while the person pane was still mounted")
	}
	paneOpen = false
	runFrame()
	if focused != 1 {
		t.Fatalf("focus after room selection = %d", focused)
	}
	runFrame() // release the first attempt's close-focus suppression
	paneOpen = true
	FocusComposerFor("new")
	runFrame()
	if focused != 1 {
		t.Fatal("existing direct message focused before pane closed")
	}
	personPaneWasOpen = true
	triggered := 0
	triggerFocus := js.FuncOf(func(js.Value, []js.Value) any { triggered++; return nil })
	defer triggerFocus.Release()
	personTrigger = js.ValueOf(map[string]any{"isConnected": true, "focus": triggerFocus})
	syncPersonFocus(false) // close restoration queues after the DM focus frame
	paneOpen = false
	runFrame()
	if focused != 2 {
		t.Fatalf("existing direct message focus = %d", focused)
	}
	runFrame() // close restoration must respect the DM focus request
	runFrame() // suppression settles after both focus frames
	if triggered != 0 {
		t.Fatalf("profile trigger regained focus after DM open: %d", triggered)
	}
}
