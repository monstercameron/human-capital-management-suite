//go:build js && wasm

package chatui

import "syscall/js"

// bindChatSearchRecent asks the client for the recent searches when the search
// box takes the cursor, so the list under the box can offer them while it is
// empty. The page cannot hand the box an onfocus handler without adding a hook
// to the workspace, so the document listens for the focus. The returned function
// removes the listener.
func bindChatSearchRecent(model Model) func() {
	doc := js.Global().Get("document")
	if !doc.Truthy() || model.Callbacks.SearchFocus == nil {
		return nil
	}
	listener := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		if target := args[0].Get("target"); target.Truthy() && target.Get("id").String() == "chat-search" {
			model.Callbacks.SearchFocus()
		}
		return nil
	})
	doc.Call("addEventListener", "focusin", listener)
	return func() {
		doc.Call("removeEventListener", "focusin", listener)
		listener.Release()
	}
}
