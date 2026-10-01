//go:build js && wasm

package main

import (
	"syscall/js"
	"testing"
)

// TestTransientPopoverListenerIgnoresNonDetailsRoot covers the shared
// selector matching the action-launcher div as well as native details menus.
func TestTransientPopoverListenerIgnoresNonDetailsRoot(t *testing.T) {
	global := js.Global()
	priorDocument := global.Get("document")
	documentExisted := priorDocument.Type() != js.TypeUndefined
	document := js.Global().Get("Object").New()
	var clickListener js.Value
	var callbacks []js.Func
	addEventListener := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 2 && args[0].String() == "click" {
			clickListener = args[1]
		}
		return nil
	})
	callbacks = append(callbacks, addEventListener)
	document.Set("addEventListener", addEventListener)
	global.Set("document", document)
	defer func() {
		for _, callback := range callbacks {
			callback.Release()
		}
		if documentExisted {
			global.Set("document", priorDocument)
		} else {
			global.Get("Reflect").Call("deleteProperty", global, "document")
		}
	}()

	controller := newBrowserTransientPopoverController()
	controller.Bind()
	if !clickListener.Truthy() {
		t.Fatal("controller did not bind its document click listener")
	}

	root := js.Global().Get("Object").New()
	root.Set("tagName", "DIV")
	target := transientPopoverTestTarget(t, root, false, &callbacks)
	clickListener.Invoke(transientPopoverTestClickEvent(target))
	if got := root.Get("open"); got.Type() != js.TypeUndefined {
		t.Fatalf("non-details launcher acquired an open property: %v", got)
	}

	root = js.Global().Get("Object").New()
	root.Set("tagName", "DETAILS")
	root.Set("open", true)
	closed := false
	getAttribute := js.FuncOf(func(_ js.Value, _ []js.Value) any { return nil })
	setAttribute := js.FuncOf(func(_ js.Value, _ []js.Value) any { return nil })
	removeAttribute := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 1 && args[0].String() == "open" {
			closed = true
			root.Set("open", false)
		}
		return nil
	})
	callbacks = append(callbacks, getAttribute, setAttribute, removeAttribute)
	root.Set("getAttribute", getAttribute)
	root.Set("setAttribute", setAttribute)
	root.Set("removeAttribute", removeAttribute)
	target = transientPopoverTestTarget(t, root, true, &callbacks)
	clickListener.Invoke(transientPopoverTestClickEvent(target))
	if !closed || root.Get("open").Bool() {
		t.Fatal("clicking a details-menu link no longer closes the native disclosure")
	}
}

func transientPopoverTestTarget(t *testing.T, root js.Value, link bool, callbacks *[]js.Func) js.Value {
	t.Helper()
	target := js.Global().Get("Object").New()
	target.Set("nodeType", 1)
	closest := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && args[0].String() == "a[href]" && link {
			return js.ValueOf(true)
		}
		return root
	})
	*callbacks = append(*callbacks, closest)
	target.Set("closest", closest)
	return target
}

func transientPopoverTestClickEvent(target js.Value) js.Value {
	event := js.Global().Get("Object").New()
	event.Set("target", target)
	event.Set("type", "click")
	return event
}
