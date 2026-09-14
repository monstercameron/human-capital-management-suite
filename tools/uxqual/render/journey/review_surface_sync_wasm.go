//go:build js && wasm

package journey

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// useReviewDetailsSync mirrors a <details> element's native "toggle" state
// into GWC by calling onToggle whenever the reader (or a script) opens or
// closes it. It is the one place review_surface.go's disclosure and its
// nested ui.Overlay agree on whether the review is open: the <details>
// element stays the single source of truth, so a plain click on its
// <summary> and an Escape-driven ui.Overlay dismissal both end up in the
// same state.
//
// The effect is bound once per instance (keyed by detailsID, not by the
// open state itself) and torn down on unmount, matching
// popover_focus_wasm.go and drawer_focus_wasm.go's own lifecycle shape in
// internal/humanwork/productui.
func useReviewDetailsSync(detailsID string, onToggle func(open bool)) {
	ui.UseEffectOf(func() func() {
		doc := js.Global().Get("document")
		el := doc.Call("getElementById", detailsID)
		if !el.Truthy() {
			return nil
		}
		listener := js.FuncOf(func(js.Value, []js.Value) any {
			onToggle(el.Get("open").Bool())
			return nil
		})
		el.Call("addEventListener", "toggle", listener)
		return func() {
			el.Call("removeEventListener", "toggle", listener)
			listener.Release()
		}
	}, detailsID)
}

// closeReviewDetails collapses the <details> element imperatively so a
// dismissal that originates from ui.Overlay (Escape, an outside click, the
// explicit Cancel button) closes the native disclosure too, keeping the
// mirrored state in useReviewDetailsSync consistent with what is on
// screen. ui.Overlay's own RestoreFocus already returns focus to whatever
// was active when the overlay opened -- in every real path, the <summary>
// trigger -- so this does not additionally move focus itself.
func closeReviewDetails(detailsID string) {
	doc := js.Global().Get("document")
	el := doc.Call("getElementById", detailsID)
	if !el.Truthy() {
		return
	}
	el.Set("open", false)
}
