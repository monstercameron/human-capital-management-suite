//go:build js && wasm

package productui

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// projectDialogClose is the latest Close callback. Each render supplies a new
// closure, and the listeners bound once per opened task call through this so
// they never hold a stale view.
var projectDialogClose func()

// useProjectTaskDialog promotes the rendered <dialog> to a native modal when
// a task opens and gives focus back to that task's card when it closes.
// showModal supplies the focus trap, makes the page inert and routes Escape
// to a cancel event; the listeners turn cancel, a backdrop click and the
// close button into one address change through close.
func useProjectTaskDialog(dialogID, taskID string, close func()) {
	projectDialogClose = close
	ui.UseEffectOf(func() func() {
		return bindProjectTaskDialog(dialogID, taskID)
	}, struct{ Dialog, Task string }{dialogID, taskID})
}

func bindProjectTaskDialog(dialogID, taskID string) func() {
	doc := js.Global().Get("document")
	dialog := doc.Call("getElementById", dialogID)
	if !dialog.Truthy() {
		return nil
	}
	func() {
		defer func() { _ = recover() }()
		if !dialog.Get("open").Bool() {
			dialog.Call("showModal")
			// Start on the close button, not the first field: a phone
			// keyboard must not open, and nothing looks pre-selected.
			if closer := dialog.Call("querySelector", ".projectui-modal-close"); closer.Truthy() {
				closer.Call("focus", map[string]any{"preventScroll": true})
			}
		}
	}()
	closing := false
	requestClose := func() {
		if closing {
			return
		}
		closing = true
		if projectDialogClose != nil {
			projectDialogClose()
		}
	}
	cancel := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			args[0].Call("preventDefault")
		}
		requestClose()
		return nil
	})
	click := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		target := args[0].Get("target")
		if !target.Truthy() {
			return nil
		}
		// A click on the dialog box itself, outside its content, is a click
		// on the backdrop.
		if target.Equal(dialog) {
			requestClose()
			return nil
		}
		if target.Get("closest").Truthy() && target.Call("closest", "[data-projectui-close]").Truthy() {
			args[0].Call("preventDefault")
			requestClose()
		}
		return nil
	})
	// showModal makes the page inert, but Tab past the last control still
	// leaves for the browser's own chrome. Wrap it at the dialog's edges.
	keydown := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || args[0].Get("key").String() != "Tab" {
			return nil
		}
		event := args[0]
		list := dialog.Call("querySelectorAll", drawerFocusableSelector)
		items := make([]js.Value, 0, list.Get("length").Int())
		for index := 0; index < list.Get("length").Int(); index++ {
			if item := list.Index(index); drawerFocusableVisible(item) {
				items = append(items, item)
			}
		}
		if len(items) == 0 {
			event.Call("preventDefault")
			dialog.Call("focus")
			return nil
		}
		first, last := items[0], items[len(items)-1]
		active := doc.Get("activeElement")
		switch {
		case event.Get("shiftKey").Bool() && (active.Equal(first) || !dialog.Call("contains", active).Bool()):
			event.Call("preventDefault")
			last.Call("focus")
		case !event.Get("shiftKey").Bool() && (active.Equal(last) || !dialog.Call("contains", active).Bool()):
			event.Call("preventDefault")
			first.Call("focus")
		}
		return nil
	})
	dialog.Call("addEventListener", "cancel", cancel)
	dialog.Call("addEventListener", "click", click)
	dialog.Call("addEventListener", "keydown", keydown)
	return func() {
		dialog.Call("removeEventListener", "cancel", cancel)
		dialog.Call("removeEventListener", "click", click)
		dialog.Call("removeEventListener", "keydown", keydown)
		cancel.Release()
		click.Release()
		keydown.Release()
		func() {
			defer func() { _ = recover() }()
			if dialog.Get("open").Bool() {
				dialog.Call("close")
			}
		}()
		focusProjectCard(taskID, 0)
	}
}

// focusProjectCard returns focus to the card that opened the modal once the
// board has re-rendered without it.
func focusProjectCard(taskID string, attempt int) {
	if attempt > 12 || taskID == "" {
		return
	}
	var frame js.Func
	frame = js.FuncOf(func(js.Value, []js.Value) any {
		defer frame.Release()
		doc := js.Global().Get("document")
		if open := doc.Call("getElementById", projectTaskDialogID); open.Truthy() && open.Get("open").Bool() {
			focusProjectCard(taskID, attempt+1)
			return nil
		}
		cards := doc.Call("querySelectorAll", ".projectui-card[data-task-id] a.projectui-card-title")
		for index := 0; index < cards.Get("length").Int(); index++ {
			link := cards.Index(index)
			card := link.Call("closest", ".projectui-card")
			if card.Truthy() && card.Call("getAttribute", "data-task-id").String() == taskID {
				link.Call("focus")
				return nil
			}
		}
		focusProjectCard(taskID, attempt+1)
		return nil
	})
	js.Global().Call("requestAnimationFrame", frame)
}
