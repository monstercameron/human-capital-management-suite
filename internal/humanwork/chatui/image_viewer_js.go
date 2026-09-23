//go:build js && wasm

package chatui

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

var imageViewerRoom string
var imageViewerPrincipal string
var imageViewerPost string
var imageViewerClose func(bool)
var imageViewerDisplayCleanup func()
var imageViewerOriginalCleanup func()

// The viewer lives on document.body so a timeline render cannot replace it or
// move the scroll position. It begins with the visible thumbnail and upgrades
// through the same protected grant when the optimized display image is ready.
func openImageViewer(event ui.Event, title, closeLabel, downloadLabel string) {
	target := event.JSValue().Get("target")
	if !target.Truthy() {
		return
	}
	button := target.Call("closest", ".attachment-image-open")
	if !button.Truthy() {
		return
	}
	thumbnail := button.Call("querySelector", "img")
	if !thumbnail.Truthy() || thumbnail.Get("src").String() == "" {
		return
	}
	if imageViewerClose != nil {
		imageViewerClose(false)
	}
	doc := js.Global().Get("document")
	workspace := button.Call("closest", ".chat-workspace")
	backdrop := doc.Call("createElement", "div")
	backdrop.Set("className", "chat-image-viewer")
	backdrop.Call("setAttribute", "role", "dialog")
	backdrop.Call("setAttribute", "aria-modal", "true")
	backdrop.Call("setAttribute", "aria-label", title)
	backdrop.Call("setAttribute", "lang", workspace.Get("lang"))
	backdrop.Call("setAttribute", "dir", workspace.Get("dir"))
	closeButton := doc.Call("createElement", "button")
	closeButton.Set("type", "button")
	closeButton.Set("className", "chat-image-viewer-close")
	closeButton.Call("setAttribute", "aria-label", closeLabel)
	closeButton.Set("textContent", "×")
	media := doc.Call("createElement", "div")
	media.Set("className", "chat-image-viewer-media")
	preview := doc.Call("createElement", "img")
	preview.Set("className", "chat-image-viewer-preview")
	preview.Set("src", thumbnail.Get("src"))
	preview.Set("alt", thumbnail.Get("alt"))
	full := doc.Call("createElement", "img")
	full.Set("className", "chat-image-viewer-full")
	full.Set("src", thumbnail.Get("src"))
	full.Set("alt", "")
	full.Call("setAttribute", "aria-hidden", "true")
	original := doc.Call("createElement", "img")
	original.Set("className", "chat-image-viewer-original")
	original.Set("alt", "")
	original.Call("setAttribute", "aria-hidden", "true")
	download := doc.Call("createElement", "button")
	download.Set("type", "button")
	download.Set("className", "chat-image-viewer-download")
	download.Set("textContent", downloadLabel)
	download.Call("setAttribute", "hidden", "")
	download.Call("setAttribute", "aria-label", downloadLabel+": "+button.Get("dataset").Get("mediaName").String())
	media.Call("appendChild", preview)
	media.Call("appendChild", full)
	media.Call("appendChild", original)
	backdrop.Call("appendChild", closeButton)
	backdrop.Call("appendChild", media)
	backdrop.Call("appendChild", download)
	doc.Get("body").Call("appendChild", backdrop)
	imageViewerRoom = workspace.Get("dataset").Get("selectedId").String()
	imageViewerPrincipal = workspace.Get("dataset").Get("principal").String()
	displayURL := button.Get("dataset").Get("mediaDisplay").String()
	if row := button.Call("closest", "[data-message-id]"); row.Truthy() {
		imageViewerPost = row.Get("dataset").Get("messageId").String()
	}
	var onClick, onKey, onMutation js.Func
	var observer js.Value
	imageViewerClose = func(restore bool) {
		if imageViewerClose == nil {
			return
		}
		imageViewerClose = nil
		imageViewerRoom = ""
		imageViewerPrincipal = ""
		imageViewerPost = ""
		if imageViewerDisplayCleanup != nil {
			imageViewerDisplayCleanup()
			imageViewerDisplayCleanup = nil
		}
		if imageViewerOriginalCleanup != nil {
			imageViewerOriginalCleanup()
			imageViewerOriginalCleanup = nil
		}
		backdrop.Call("removeEventListener", "click", onClick)
		doc.Call("removeEventListener", "keydown", onKey, true)
		observer.Call("disconnect")
		backdrop.Call("remove")
		onClick.Release()
		onKey.Release()
		onMutation.Release()
		if restore {
			focus := button
			if !focus.Get("isConnected").Bool() {
				buttons := doc.Call("querySelectorAll", ".attachment-image-open")
				for i := 0; i < buttons.Get("length").Int(); i++ {
					candidate := buttons.Index(i)
					if candidate.Get("dataset").Get("id").String() == button.Get("dataset").Get("id").String() {
						focus = candidate
						break
					}
				}
			}
			if focus.Get("isConnected").Bool() {
				focus.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
			}
		}
	}
	onClick = js.FuncOf(func(_ js.Value, args []js.Value) any {
		clicked := args[0].Get("target")
		if clicked.Equal(download) {
			if bridge := js.Global().Get("hcmChatMediaDownload"); bridge.Type() == js.TypeFunction {
				bridge.Invoke(imageViewerPost, button.Get("dataset").Get("mediaId").String())
			}
			return nil
		}
		if clicked.Equal(backdrop) || clicked.Equal(closeButton) {
			imageViewerClose(true)
		}
		return nil
	})
	onKey = js.FuncOf(func(_ js.Value, args []js.Value) any {
		key := args[0].Get("key").String()
		if key == "Escape" {
			args[0].Call("preventDefault")
			args[0].Call("stopPropagation")
			imageViewerClose(true)
		} else if key == "Tab" {
			if !download.Get("hidden").Bool() {
				args[0].Call("preventDefault")
				if args[0].Get("shiftKey").Truthy() {
					if js.Global().Get("document").Get("activeElement").Equal(closeButton) {
						download.Call("focus")
					} else {
						closeButton.Call("focus")
					}
				} else if js.Global().Get("document").Get("activeElement").Equal(download) {
					closeButton.Call("focus")
				} else {
					download.Call("focus")
				}
			} else {
				args[0].Call("preventDefault")
				closeButton.Call("focus")
			}
		}
		return nil
	})
	onMutation = js.FuncOf(func(js.Value, []js.Value) any {
		if !workspace.Get("isConnected").Bool() ||
			workspace.Get("dataset").Get("selectedId").String() != imageViewerRoom ||
			workspace.Get("dataset").Get("principal").String() != imageViewerPrincipal ||
			!button.Get("isConnected").Bool() || button.Get("dataset").Get("mediaDisplay").String() != displayURL {
			imageViewerClose(false)
		}
		return nil
	})
	observer = js.Global().Get("MutationObserver").New(onMutation)
	observer.Call("observe", doc.Get("body"), js.ValueOf(map[string]any{"childList": true, "subtree": true, "attributes": true, "attributeFilter": []any{"data-selected-id", "data-principal", "data-media-display"}}))
	backdrop.Call("addEventListener", "click", onClick)
	doc.Call("addEventListener", "keydown", onKey, true)
	closeButton.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
	imageViewerDisplayCleanup = loadChatImageDisplay(button, full, func(ok bool) {
		if !ok {
			if js.Global().Get("hcmChatMediaDownload").Type() == js.TypeFunction {
				download.Call("removeAttribute", "hidden")
			}
			return
		}
		imageViewerOriginalCleanup = loadChatImageOriginal(button, original, func(loaded, downloadOnly bool) {
			if loaded {
				if imageViewerDisplayCleanup != nil {
					imageViewerDisplayCleanup()
					imageViewerDisplayCleanup = nil
				}
			} else if downloadOnly && js.Global().Get("hcmChatMediaDownload").Type() == js.TypeFunction {
				download.Call("removeAttribute", "hidden")
			}
		})
	})
}

func syncImageViewer(selectedID, principal string) {
	if imageViewerClose != nil && (selectedID != imageViewerRoom || principal != imageViewerPrincipal) {
		imageViewerClose(false)
	}
}
