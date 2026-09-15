//go:build js && wasm

package productui

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Keep the complete SSR tree as the no-JavaScript fallback. A large tree
// switches to the bounded desktop viewport only after the browser mounts it.
func useOrganizationVirtualViewport(eligible bool) bool {
	enabled := ui.UseState(false)
	ui.UseEffect(func() func() {
		if !eligible {
			enabled.Set(false)
			return nil
		}
		media := js.Global().Call("matchMedia", "(min-width: 761px)")
		enabled.Set(media.Get("matches").Bool())
		listener := js.FuncOf(func(_ js.Value, _ []js.Value) any {
			enabled.Set(media.Get("matches").Bool())
			return nil
		})
		media.Call("addEventListener", "change", listener)
		return func() {
			media.Call("removeEventListener", "change", listener)
			listener.Release()
		}
	}, eligible)
	return enabled.Get()
}

func organizationScrollTop(event ui.Event) float64 {
	target := event.JSValue().Get("currentTarget")
	if !target.Truthy() {
		return 0
	}
	return target.Get("scrollTop").Float()
}

func useOrganizationVirtualScrollPosition(top float64) {
	ui.UseLayoutEffect(func() func() {
		tree := js.Global().Get("document").Call("getElementById", "organization-virtual-tree")
		if tree.Truthy() && tree.Get("scrollTop").Float() != top {
			tree.Set("scrollTop", top)
		}
		return nil
	}, top)
}
