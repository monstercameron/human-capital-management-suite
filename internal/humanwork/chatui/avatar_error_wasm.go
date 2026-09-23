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

func initChatPhotoHandlers() {
	photoErrorHandler = ui.WrapHandler(func(event ui.Event) {
		setChatPhotoVisible(event.JSValue().Get("target"), false)
	})
	photoLoadHandler = ui.WrapHandler(func(event ui.Event) {
		setChatPhotoVisible(event.JSValue().Get("target"), true)
	})
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
