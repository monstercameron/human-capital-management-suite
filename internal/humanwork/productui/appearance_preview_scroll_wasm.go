//go:build js && wasm

package productui

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// useAppearancePreviewScrollReset returns each preview page to its beginning.
// Without this, the persistent dialog scrollport opens the next page at the
// previous page's offset, often hiding its heading and primary content.
func useAppearancePreviewScrollReset(page PageID, open bool) {
	ui.UseEffectOf(func() func() {
		if !open {
			return nil
		}
		dialog := js.Global().Get("document").Call("getElementById", "appearance-preview-dialog")
		if dialog.Truthy() {
			viewport := dialog.Call("querySelector", ".appearance-preview-live")
			if viewport.Truthy() {
				viewport.Set("scrollTop", 0)
			}
		}
		return nil
	}, struct {
		Page PageID
		Open bool
	}{page, open})
}

func requestAppearancePreview() {
	button := js.Global().Get("document").Call("getElementById", "appearance-preview-open")
	if button.Truthy() {
		button.Call("click")
	}
}
