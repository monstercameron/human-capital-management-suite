//go:build js && wasm

package chatui

import "syscall/js"

// chatbug090ScrollTargetDismisses classifies the target of a scroll event for
// chatbug090RailMenuScrollDismisses. A document or window scroll has no closest().
func chatbug090ScrollTargetDismisses(target js.Value) bool {
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return chatbug090RailMenuScrollDismisses(false, false, true)
	}
	return chatbug090RailMenuScrollDismisses(target.Call("closest", ".rail-row-menu").Truthy(), target.Call("closest", ".chat-rail").Truthy(), false)
}
