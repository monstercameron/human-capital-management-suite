//go:build js && wasm

package chatui

import (
	"strconv"
	"syscall/js"
)

// chatbug090ScrollTargetDismisses classifies the target of a scroll event for
// chatbug090RailMenuScrollDismisses. A document or window scroll has no closest().
func chatbug090ScrollTargetDismisses(target js.Value) bool {
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return chatbug090RailMenuScrollDismisses(false, false, true)
	}
	return chatbug090RailMenuScrollDismisses(target.Call("closest", ".rail-row-menu").Truthy(), target.Call("closest", ".chat-rail").Truthy(), false)
}

// chatux040RailMenuLayout measures an open row or section menu at the width its
// longest item asks for, writes that width to the layer (the height it needs is
// read at it) and returns it with the rectangle the menu must stay inside:
// the sidebar's column and the page to its right, below the sidebar's head.
func chatux040RailMenuLayout(layer, root js.Value, bounds chatLayerRect, viewportWidth float64, rtl bool) (float64, chatLayerRect) {
	style := layer.Get("style")
	style.Call("setProperty", "width", "max-content")
	style.Call("setProperty", "max-width", strconv.FormatFloat(chatux040MenuMax, 'f', 0, 64)+"px")
	natural := layer.Get("offsetWidth").Float()
	style.Call("removeProperty", "max-width")
	width := chatux040RailMenuFit(natural, viewportWidth)
	style.Call("setProperty", "width", strconv.FormatFloat(width, 'f', 2, 64)+"px")
	measure := func(selector string) chatLayerRect {
		if !root.Truthy() {
			return chatLayerRect{}
		}
		el := root.Call("querySelector", selector)
		if !el.Truthy() {
			return chatLayerRect{}
		}
		box := el.Call("getBoundingClientRect")
		return chatLayerRect{box.Get("left").Float(), box.Get("top").Float(), box.Get("right").Float(), box.Get("bottom").Float()}
	}
	return width, chatux040RailMenuBounds(bounds, measure(".chat-rail"), measure(".chat-rail .rail-fixed"), viewportWidth, rtl)
}
