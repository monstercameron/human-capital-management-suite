//go:build js && wasm

package chatui

import (
	"syscall/js"
	"testing"
)

// TestChatImageLoaderRebindsOnRoomChange pins round 3 C-4 (live): a room
// switch used to close the loader when the selected-id mutation reached it,
// and when the next room's layout effect had already reused that loader the
// new room was left with no loader at all -- src-less GIF tiles showing the
// broken-image glyph. The loader now drops the old room's records and adopts
// the new room, and the old effect's cleanup leaves it alone.
func TestChatImageLoaderRebindsOnRoomChange(t *testing.T) {
	globals := []string{"IntersectionObserver", "MutationObserver", "__chatTestObservers"}
	old := make(map[string]js.Value, len(globals))
	for _, key := range globals {
		old[key] = js.Global().Get(key)
	}
	defer func() {
		if activeChatImageLoader != nil {
			activeChatImageLoader.close()
		}
		for _, key := range globals {
			js.Global().Set(key, old[key])
		}
	}()
	js.Global().Set("__chatTestObservers", js.Global().Get("Array").New())
	js.Global().Set("IntersectionObserver", js.Global().Get("Function").New("callback", "opts", `
		this.callback=callback; this.seen=[];
		this.observe=function(v){this.seen.push(v)};
		this.unobserve=function(){}; this.disconnect=function(){};
		globalThis.__chatTestObservers.push(this);
	`))
	js.Global().Set("MutationObserver", js.Global().Get("Function").New("callback", "this.observe=function(){};this.disconnect=function(){}"))

	image := js.Global().Get("Object").New()
	remove := js.FuncOf(func(js.Value, []js.Value) any { return nil })
	defer remove.Release()
	image.Set("removeAttribute", remove)
	button := js.Global().Get("Object").New()
	button.Set("isConnected", true)
	button.Set("dataset", js.ValueOf(map[string]any{"mediaId": "gif", "mediaThumb": "thumbnail", "mediaDisplay": "display"}))
	queryImage := js.FuncOf(func(js.Value, []js.Value) any { return image })
	defer queryImage.Release()
	button.Set("querySelector", queryImage)
	buttons := js.Global().Get("Array").New()
	buttons.Call("push", button)
	root := js.Global().Get("Object").New()
	root.Set("isConnected", true)
	root.Set("dataset", js.ValueOf(map[string]any{"selectedId": "room-a", "principal": "viewer"}))
	queryButtons := js.FuncOf(func(js.Value, []js.Value) any { return buttons })
	defer queryButtons.Release()
	root.Set("querySelectorAll", queryButtons)

	initChatImageLoading(root)
	loader := activeChatImageLoader
	if loader == nil || loader.room != "room-a" {
		t.Fatal("loader did not start for the first room")
	}
	cleanupA := func() {
		if activeChatImageLoader == loader && loader.room == "room-a" && loader.principal == "viewer" {
			loader.close()
		}
	}

	root.Set("dataset", js.ValueOf(map[string]any{"selectedId": "room-b", "principal": "viewer"}))
	fresh := js.Global().Get("Object").New()
	fresh.Set("isConnected", true)
	fresh.Set("dataset", js.ValueOf(map[string]any{"mediaId": "gif-b", "mediaThumb": "thumbnail", "mediaDisplay": "display"}))
	fresh.Set("querySelector", queryImage)
	buttons.SetIndex(0, fresh)
	initChatImageLoading(root) // the next room's effect runs first ...
	cleanupA()                 // ... and the previous room's cleanup after it
	if activeChatImageLoader != loader || loader.room != "room-b" {
		t.Fatalf("room change left no loader for the new room (active=%v)", activeChatImageLoader != nil)
	}
	if fresh.Get("__chatImageRecordKey").Type() != js.TypeString {
		t.Fatal("the new room's attachment was not registered for loading")
	}
}
