//go:build js && wasm

package journey

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// useFocusOnMount moves focus to targetID the moment the fiber calling it
// mounts, and not again after (see focusOnMount in mount_focus.go for why it
// must only ever be called from within that component's own fiber, never as
// a plain nested call).
//
// PROMOUX-010's residual, measured live: Start swaps Page.List for
// Page.Proposal wholesale (out of scope to re-architect -- see components.go
// and review_surface.go's own doc comments), which drops focus to <body>
// and leaves the review surface's own "Review and propose" trigger however
// far down the fresh document happens to render, with nothing bringing it
// into view. The first version of this fix targeted the new page's own
// heading instead of the trigger; live measurement showed the heading
// itself already sits inside the viewport, so focusing it cannot scroll
// anything into view -- it closes neither the lost-focus clause (see
// mount_focus.go) nor the below-the-fold one. Targeting the trigger closes
// both: a programmatic .focus() scrolls its target into view as a browser
// side effect, and the trigger is the element that is actually below the
// fold.
//
// targetID is whatever compile-time-constant id the caller (proposalView,
// via focusOnMount) passes; because it is a constant across every render of
// the same mounted fiber, typing in a field or the subject finishing its
// background load does not change it and so does not re-run this effect,
// only a fresh mount does.
func useFocusOnMount(targetID string) {
	ui.UseEffectOf(func() func() {
		doc := js.Global().Get("document")
		el := doc.Call("getElementById", targetID)
		if el.Truthy() {
			el.Call("focus")
		}
		return nil
	}, targetID)
}
