//go:build js && wasm

package chatui

import (
	"strconv"
	"syscall/js"
)

// loadChatImageOriginal upgrades an already-visible display rendition after
// the viewer was opened. It refuses unknown or over-budget image metadata so
// the caller can offer the protected byte-exact download URL instead.
func loadChatImageOriginal(button, originalImage js.Value, ready func(loaded, downloadOnly bool)) func() {
	if !button.Truthy() || !originalImage.Truthy() {
		return func() {}
	}
	dataset := button.Get("dataset")
	url := dataset.Get("mediaOriginal").String()
	width, widthErr := strconv.Atoi(dataset.Get("mediaWidth").String())
	height, heightErr := strconv.Atoi(dataset.Get("mediaHeight").String())
	sourceBytes, bytesErr := strconv.ParseInt(dataset.Get("mediaBytes").String(), 10, 64)
	if url != "original" && !validChatOriginalURL(url) {
		ready(false, true)
		return func() {}
	}
	if dataset.Get("mediaAnimated").String() == "true" {
		// The backend's display variant for GIF is already the original animated
		// byte stream; another full source Blob only duplicates animation memory.
		ready(false, true)
		return func() {}
	}
	if widthErr != nil || heightErr != nil || bytesErr != nil || !chatImageOriginalFitsViewerBounds(width, height, sourceBytes) {
		ready(false, true)
		return func() {}
	}
	workspace := button.Call("closest", ".chat-workspace")
	if !workspace.Truthy() {
		ready(false, true)
		return func() {}
	}
	room := workspace.Get("dataset").Get("selectedId").String()
	principal := workspace.Get("dataset").Get("principal").String()
	controller := js.Global().Get("AbortController").New()
	closed, settled := false, false
	objectURL := ""
	var onload, onerror js.Func
	finish := func(loaded bool) {
		if settled {
			return
		}
		settled = true
		if onload.Value.Truthy() {
			originalImage.Call("removeEventListener", "load", onload)
			originalImage.Call("removeEventListener", "error", onerror)
			onload.Release()
			onerror.Release()
		}
		if loaded && !closed {
			originalImage.Get("classList").Call("add", "chat-image-original-ready")
		}
		if !closed {
			ready(loaded, !loaded)
		}
	}
	startProtectedImageFetch(button, "original", url, controller.Get("signal"), func(fetched string) {
		if closed || !button.Get("isConnected").Truthy() || !workspace.Get("isConnected").Truthy() ||
			workspace.Get("dataset").Get("selectedId").String() != room ||
			workspace.Get("dataset").Get("principal").String() != principal ||
			dataset.Get("mediaOriginal").String() != url {
			if fetched != "" {
				revokeChatImageURL(fetched)
			}
			return
		}
		if fetched == "" {
			finish(false)
			return
		}
		objectURL = fetched
		onload = js.FuncOf(func(js.Value, []js.Value) any { finish(true); return nil })
		onerror = js.FuncOf(func(js.Value, []js.Value) any {
			revokeChatImageURL(objectURL)
			objectURL = ""
			finish(false)
			return nil
		})
		originalImage.Call("addEventListener", "load", onload, js.ValueOf(map[string]any{"once": true}))
		originalImage.Call("addEventListener", "error", onerror, js.ValueOf(map[string]any{"once": true}))
		originalImage.Set("decoding", "async")
		originalImage.Set("src", objectURL)
	})
	return func() {
		if closed {
			return
		}
		closed = true
		controller.Call("abort")
		if !settled {
			settled = true
			if onload.Value.Truthy() {
				originalImage.Call("removeEventListener", "load", onload)
				originalImage.Call("removeEventListener", "error", onerror)
				onload.Release()
				onerror.Release()
			}
		}
		originalImage.Get("classList").Call("remove", "chat-image-original-ready")
		originalImage.Call("removeAttribute", "src")
		if objectURL != "" {
			revokeChatImageURL(objectURL)
		}
	}
}

func validChatOriginalURL(raw string) bool {
	original, ok := chatMediaOriginalURL(raw)
	return ok && original == raw
}
