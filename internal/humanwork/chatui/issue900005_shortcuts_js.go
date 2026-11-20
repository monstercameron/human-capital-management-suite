//go:build js && wasm

package chatui

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// issue900005OpenShortcuts opens the shortcuts sheet from the Keyboard shortcuts
// row of Chat preferences. The panel closes first and focus returns to its
// gear, so the sheet remembers a control that is still on the page and Esc
// brings focus back to it.
func issue900005OpenShortcuts(event ui.Event, locale string) {
	target := event.JSValue().Get("target")
	if target.Truthy() && target.Get("closest").Type() == js.TypeFunction {
		if layer := target.Call("closest", "[data-chat-layer]"); layer.Truthy() {
			closeChatDisclosureLayer(layer, true)
		}
	}
	openChatShortcutSheet(locale)
}
