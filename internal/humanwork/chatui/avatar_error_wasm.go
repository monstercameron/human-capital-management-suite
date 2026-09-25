//go:build js && wasm

package chatui

import (
	"sync"
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Initialize after the DOM runtime exists. Wrapping at package init leaves a
// raw Go function in the img prop, which the browser cannot convert to js.Value.
var photoHandlersOnce sync.Once
var photoErrorHandler, photoLoadHandler ui.Handler
var attachmentImageErrorHandlerOnce sync.Once
var attachmentImageErrorHandler ui.Handler

func initChatPhotoHandlers() {
	photoErrorHandler = ui.WrapHandler(func(event ui.Event) {
		setChatPhotoVisible(event.JSValue().Get("target"), false)
	})
	photoLoadHandler = ui.WrapHandler(func(event ui.Event) {
		setChatPhotoVisible(event.JSValue().Get("target"), true)
	})
}

// initChatAttachmentImageErrorHandler marks the enclosing attachment tile
// "failed" so the stylesheet swaps in the pre-rendered .attachment-fallback
// block (C-2). The DOM is never mutated imperatively here -- only a class is
// toggled -- because the fallback markup already exists in the tree from the
// server render and a sibling re-render would otherwise wipe an
// innerHTML-built replacement.
func initChatAttachmentImageErrorHandler() {
	attachmentImageErrorHandler = ui.WrapHandler(func(event ui.Event) {
		markChatAttachmentImageFailed(event.JSValue().Get("target"))
	})
}

// markChatAttachmentImageFailed is the class toggle both the <img>'s onerror
// handler and the loader's own stuck-load watchdog use (C-2 live re-check):
// an <img> left with an empty src never fires "error" -- there is nothing
// to fail to load -- so a fetch that never resolves needed a second way to
// reach the same visible state as a fetch that resolved and then failed.
func markChatAttachmentImageFailed(target js.Value) {
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return
	}
	figure := target.Call("closest", ".attachment-image")
	if !figure.Truthy() {
		return
	}
	classList := figure.Get("classList")
	if classList.Truthy() && classList.Get("add").Type() == js.TypeFunction {
		classList.Call("add", "failed")
	}
}

// A retried thumbnail keeps the fallback visible until the browser decodes
// the replacement image. Only then can the image tile replace the fallback.
func markChatAttachmentImageLoaded(target js.Value) {
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return
	}
	figure := target.Call("closest", ".attachment-image")
	if !figure.Truthy() {
		return
	}
	classList := figure.Get("classList")
	if classList.Truthy() && classList.Get("remove").Type() == js.TypeFunction {
		classList.Call("remove", "failed")
	}
}

func chatAttachmentImageErrorHandler() ui.Handler {
	attachmentImageErrorHandlerOnce.Do(initChatAttachmentImageErrorHandler)
	return attachmentImageErrorHandler
}

func setChatPhotoVisible(target js.Value, visible bool) {
	if !target.Truthy() {
		return
	}
	if visible {
		target.Get("style").Set("display", "")
	} else {
		target.Get("style").Set("display", "none")
	}
}

func chatPhotoErrorHandler() ui.Handler {
	photoHandlersOnce.Do(initChatPhotoHandlers)
	return photoErrorHandler
}
func chatPhotoLoadHandler() ui.Handler {
	photoHandlersOnce.Do(initChatPhotoHandlers)
	return photoLoadHandler
}
