//go:build js && wasm

package chatui

import (
	"strconv"
	"syscall/js"
)

func chatMeasuredRect(node js.Value) chatLayerRect {
	box := node.Call("getBoundingClientRect")
	return chatLayerRect{box.Get("left").Float(), box.Get("top").Float(), box.Get("right").Float(), box.Get("bottom").Float()}
}

// chatComposerPlacementFor places a layer of a composer kind opened from a
// composer (the main one or a thread's) wholly above it. It reports false for
// any other layer, and for an opener that is gone or not in a composer, which
// the general placement handles.
func chatComposerPlacementFor(opener js.Value, kind string, bounds chatLayerRect, width, need, viewportHeight float64, rtl bool) (chatLayerPlacement, bool) {
	if !chatComposerLayerKind(kind) || !opener.Truthy() || !opener.Get("isConnected").Truthy() {
		return chatLayerPlacement{}, false
	}
	composerNode := opener.Call("closest", ".chat-composer,.thread-composer")
	if !composerNode.Truthy() {
		return chatLayerPlacement{}, false
	}
	composer := chatMeasuredRect(composerNode)
	if !chatLayerRectUsable(composer) {
		return chatLayerPlacement{}, false
	}
	// The voice and location openers are drawn as nothing over the + button;
	// without a laid-out opener the layer starts at the composer's inline start.
	from := chatMeasuredRect(opener)
	if !chatLayerRectUsable(from) {
		from = composer
	}
	return chatComposerLayerPlacement(from, composer, bounds, width, need, viewportHeight, rtl), true
}

// syncChatComposerRoom tells the composer's completion lists ("@", documents,
// "/") how much room there is above the composer, under the conversation
// header, as --chat-composer-room; they scroll inside it.
func syncChatComposerRoom(root js.Value) {
	lists := root.Call("querySelectorAll", ".mention-menu,.command-menu")
	for i := 0; i < lists.Length(); i++ {
		list := lists.Index(i)
		composerNode := list.Call("closest", ".chat-composer,.thread-composer")
		if !composerNode.Truthy() {
			continue
		}
		composer := chatMeasuredRect(composerNode)
		if !chatLayerRectUsable(composer) {
			continue
		}
		area := chatLayerArea(root, composerNode)
		list.Get("style").Call("setProperty", "--chat-composer-room", strconv.FormatFloat(chatComposerRoom(composer, area), 'f', 0, 64)+"px")
	}
}

// closeStaleChatPopovers takes a layer that was hidden by its owner out of the
// top layer: an owner that sets the hidden attribute (the GIF picker) knows
// nothing of popovers.
func closeStaleChatPopovers(root js.Value) {
	stale := root.Call("querySelectorAll", "[data-chat-layer][hidden]")
	for i := 0; i < stale.Length(); i++ {
		if layer := stale.Index(i); layer.Call("matches", ":popover-open").Bool() {
			layer.Call("hidePopover")
		}
	}
}
