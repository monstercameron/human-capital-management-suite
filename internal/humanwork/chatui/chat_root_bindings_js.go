//go:build js && wasm

package chatui

var (
	chatRootBindingCleanups []func()
	chatRootBoundRoom       string
	chatRootRenderTick      uint64
)

// nextChatRenderTick gives the binding effect a dependency that changes with
// every render, so the check below runs after every commit.
func nextChatRenderTick() uint64 {
	chatRootRenderTick++
	return chatRootRenderTick
}

// rebindChatRootListeners keeps the workspace's delegated listeners (disclosure
// toggles, Escape, outside clicks, hover rows, long press) on the workspace
// element that is on the page now. The first paint after a load replaces that
// element without the conversation changing, and listeners bound to the old
// element die with it: every expandable row then does nothing until the person
// opens another conversation. The element carries a mark, so the listeners are
// bound once per element and once per conversation, never twice.
func rebindChatRootListeners(local localStore, model Model) {
	// CHATUX-001: a details section the header asked for is shown once the
	// render that draws it has committed.
	chatux001Settle()
	root := chatLayerRoot()
	if !root.Truthy() {
		return
	}
	if root.Get("__chatRootBound").Truthy() && chatRootBoundRoom == model.SelectedID {
		return
	}
	releaseChatRootListeners()
	for _, cleanup := range []func(){bindChatMessageLongPress(model), bindChatActiveRows(local), bindChatRowActionGuard(local), bindChatSearchShortcut(model)} {
		if cleanup != nil {
			chatRootBindingCleanups = append(chatRootBindingCleanups, cleanup)
		}
	}
	root.Set("__chatRootBound", true)
	chatRootBoundRoom = model.SelectedID
}

func releaseChatRootListeners() {
	for _, cleanup := range chatRootBindingCleanups {
		cleanup()
	}
	chatRootBindingCleanups = chatRootBindingCleanups[:0]
	if root := chatLayerRoot(); root.Truthy() {
		root.Set("__chatRootBound", false)
	}
}
