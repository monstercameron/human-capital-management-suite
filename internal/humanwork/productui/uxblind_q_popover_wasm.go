//go:build js && wasm

package productui

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const uxblindQTransientSelector = "details[data-hcm-transient-popover]"

func useUXBlindQPopoverController() {
	ui.UseEffectOf(func() func() {
		return bindUXBlindQPopoverController()
	}, "uxblind-q-popover-controller")
}

// useUXBlindQPopoverDismissal lets stateful popovers, such as the action
// launcher, join the document controller without installing a second set of
// outside-click and Escape listeners. Native details menus are closed directly
// by the controller; stateful surfaces receive this event and close through
// their owning component state.
func useUXBlindQPopoverDismissal(kind string, open bool, dismiss func(), triggerID string) {
	ui.UseEffectOf(func() func() {
		if !open || dismiss == nil {
			return nil
		}
		doc := js.Global().Get("document")
		if !doc.Truthy() || doc.Get("addEventListener").Type() != js.TypeFunction {
			return nil
		}
		listener := js.FuncOf(func(_ js.Value, args []js.Value) any {
			defer func() { _ = recover() }()
			if len(args) == 0 {
				return nil
			}
			detail := args[0].Get("detail")
			eventKind := ""
			if detail.Truthy() {
				eventKind = detail.Get("kind").String()
			}
			if eventKind != "" && eventKind != kind {
				return nil
			}
			dismiss()
			focusElementByID(doc, triggerID)
			return nil
		})
		doc.Call("addEventListener", uxblindQDismissEvent, listener)
		return func() {
			doc.Call("removeEventListener", uxblindQDismissEvent, listener)
			listener.Release()
		}
	}, struct {
		Kind string
		Open bool
	}{kind, open})
}

func bindUXBlindQPopoverController() func() {
	doc := js.Global().Get("document")
	if !doc.Truthy() || doc.Get("querySelectorAll").Type() != js.TypeFunction {
		return nil
	}

	closeAll := func(restoreFocus bool) {
		closeUXBlindQPopovers(doc, restoreFocus)
		uxblindQEmitDismiss(doc, "", restoreFocus)
	}
	pointer := js.FuncOf(func(_ js.Value, args []js.Value) any {
		defer func() { _ = recover() }()
		if len(args) == 0 {
			return nil
		}
		target := args[0].Get("target")
		root := uxblindQClosest(target, uxblindQTransientSelector)
		if uxblindQClosest(target, "summary").Truthy() && uxblindQHasOpenActionLauncher(doc) {
			uxblindQEmitDismiss(doc, "action-launcher", false)
		}
		if !root.Truthy() {
			if uxblindQClosest(target, "#action-launcher.action-launcher-open").Truthy() {
				closeUXBlindQPopovers(doc, true)
				return nil
			}
			closeAll(true)
			return nil
		}
		// A pointer down on another summary must close the previous menu before
		// the browser's native details toggle opens the new one.
		if !uxblindQClosest(target, "summary").Truthy() && !root.Get("open").Bool() {
			return nil
		}
		return nil
	})
	click := js.FuncOf(func(_ js.Value, args []js.Value) any {
		defer func() { _ = recover() }()
		if len(args) == 0 {
			return nil
		}
		target := args[0].Get("target")
		root := uxblindQClosest(target, uxblindQTransientSelector)
		if !root.Truthy() {
			if actionRoot := uxblindQClosest(target, "#action-launcher.action-launcher-open"); actionRoot.Truthy() {
				insideSurface := uxblindQClosest(target, "[data-hcm-popover-surface]").Truthy()
				selectable := uxblindQClosest(target, "a,button,[role=menuitem],[role=option]").Truthy()
				if insideSurface && uxblindQShouldDismiss(uxblindQEventSelect, true, selectable) {
					uxblindQEmitDismiss(doc, "action-launcher", false)
				}
				return nil
			}
			closeAll(true)
			return nil
		}
		if uxblindQClosest(target, "summary").Truthy() {
			if uxblindQHasOpenActionLauncher(doc) {
				uxblindQEmitDismiss(doc, "action-launcher", false)
			}
			closeUXBlindQSiblings(doc, root)
			return nil
		}
		insideSurface := uxblindQClosest(target, "[data-hcm-popover-surface]").Truthy()
		selectable := uxblindQClosest(target, "a,button,[role=menuitem],[role=option]").Truthy()
		if insideSurface && uxblindQShouldDismiss(uxblindQEventSelect, true, selectable) {
			closeUXBlindQRoot(root, false)
		}
		return nil
	})
	keydown := js.FuncOf(func(_ js.Value, args []js.Value) any {
		defer func() { _ = recover() }()
		if len(args) > 0 && args[0].Get("key").String() == "Escape" && uxblindQHasOpenPopover(doc) {
			args[0].Call("preventDefault")
			closeAll(true)
		}
		return nil
	})
	doc.Call("addEventListener", "pointerdown", pointer, true)
	doc.Call("addEventListener", "click", click)
	doc.Call("addEventListener", "keydown", keydown)

	var observer js.Value
	var observerCallback js.Func
	observerActive := false
	if mutationObserver := js.Global().Get("MutationObserver"); mutationObserver.Truthy() {
		observerCallback = js.FuncOf(func(js.Value, []js.Value) any {
			defer func() { _ = recover() }()
			if uxblindQVisibleDialog(doc) {
				closeAll(false)
			}
			return nil
		})
		observer = mutationObserver.New(observerCallback)
		observerActive = true
		body := doc.Get("body")
		if body.Truthy() {
			observer.Call("observe", body, js.ValueOf(uxblindQObserverOptions()))
		}
	}

	return func() {
		defer func() { _ = recover() }()
		doc.Call("removeEventListener", "pointerdown", pointer, true)
		doc.Call("removeEventListener", "click", click)
		doc.Call("removeEventListener", "keydown", keydown)
		if observerActive {
			observer.Call("disconnect")
		}
		pointer.Release()
		click.Release()
		keydown.Release()
		if observerActive {
			observerCallback.Release()
		}
	}
}

func uxblindQClosest(value js.Value, selector string) (result js.Value) {
	result = js.Null()
	defer func() { _ = recover() }()
	if value.Truthy() && value.Get("closest").Type() == js.TypeFunction {
		return value.Call("closest", selector)
	}
	return result
}

func uxblindQEmitDismiss(doc js.Value, kind string, restoreFocus bool) {
	defer func() { _ = recover() }()
	constructor := js.Global().Get("CustomEvent")
	if !doc.Truthy() || !constructor.Truthy() {
		return
	}
	event := constructor.New(uxblindQDismissEvent, js.ValueOf(map[string]any{
		"detail": map[string]any{"kind": kind, "restoreFocus": restoreFocus},
	}))
	doc.Call("dispatchEvent", event)
}

func closeUXBlindQPopovers(doc js.Value, restoreFocus bool) {
	list := doc.Call("querySelectorAll", uxblindQTransientSelector)
	var focus js.Value
	active := doc.Get("activeElement")
	for i := 0; i < list.Length(); i++ {
		root := list.Index(i)
		if !root.Get("open").Bool() {
			continue
		}
		if !focus.Truthy() || active.Truthy() && root.Call("contains", active).Bool() {
			focus = root.Call("querySelector", "summary")
		}
		root.Set("open", false)
	}
	if restoreFocus && focus.Truthy() {
		focus.Call("focus")
	}
}

func uxblindQHasOpenPopover(doc js.Value) bool {
	list := doc.Call("querySelectorAll", uxblindQTransientSelector)
	for i := 0; i < list.Length(); i++ {
		if list.Index(i).Get("open").Bool() {
			return true
		}
	}
	return uxblindQHasOpenActionLauncher(doc)
}

func uxblindQHasOpenActionLauncher(doc js.Value) bool {
	return doc.Call("querySelector", "#action-launcher.action-launcher-open").Truthy()
}

func closeUXBlindQSiblings(doc, selected js.Value) {
	list := doc.Call("querySelectorAll", uxblindQTransientSelector)
	for i := 0; i < list.Length(); i++ {
		root := list.Index(i)
		if root.Truthy() && root.Get("open").Bool() && !root.Call("isSameNode", selected).Bool() {
			root.Set("open", false)
		}
	}
}

func closeUXBlindQRoot(root js.Value, restoreFocus bool) {
	if !root.Truthy() || !root.Get("open").Bool() {
		return
	}
	root.Set("open", false)
	if restoreFocus {
		if summary := root.Call("querySelector", "summary"); summary.Truthy() {
			summary.Call("focus")
		}
	}
}

func uxblindQVisibleDialog(doc js.Value) bool {
	dialogs := doc.Call("querySelectorAll", `[role="dialog"]:not(#action-launcher-dialog):not([hidden]):not([aria-hidden="true"])`)
	for i := 0; i < dialogs.Length(); i++ {
		// A disclosure panel's role=dialog describes its popover surface; it
		// is not a modal that should dismiss the disclosure when its open
		// attribute changes. Closed non-popover details also keep descendants
		// in the DOM even though those descendants are not rendered.
		dialog := dialogs.Index(i)
		if uxblindQClosest(dialog, "details[data-hcm-transient-popover]").Truthy() {
			continue
		}
		if !uxblindQClosest(dialog, "details:not([open])").Truthy() {
			return true
		}
	}
	return false
}
