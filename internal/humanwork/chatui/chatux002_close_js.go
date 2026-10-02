//go:build js && wasm

package chatui

// chatux002CloseSidebarPanels closes the Chat preferences panel and the
// Channels menu, whichever is open, once what was done in it is finished (a new
// section was created). Nothing waits for a frame or a timer.
func chatux002CloseSidebarPanels() {
	root := chatLayerRoot()
	if !root.Truthy() {
		return
	}
	layers := root.Call("querySelectorAll", "[data-chat-layer]:not([hidden])")
	for i := 0; i < layers.Get("length").Int(); i++ {
		layer := layers.Index(i)
		if chatLayerGroup(layer.Get("dataset").Get("chatLayer").String()) == chatSidebarSettingsGroup {
			closeChatDisclosureLayer(layer, false)
		}
	}
}
