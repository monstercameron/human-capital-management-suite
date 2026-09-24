//go:build js && wasm

package productui

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const workListDetailNarrowQuery = "(max-width: 760px)"

// bindWorkLayoutViewport keeps the component's pane decision aligned with
// the real viewport. The matchMedia change event also fires when a browser
// window is resized or moved between displays.
func bindWorkLayoutViewport(state ui.State[ListDetailLayout]) {
	ui.UseEffect(func() func() {
		window := js.Global()
		if window.Get("matchMedia").Type() != js.TypeFunction {
			return nil
		}
		media := window.Call("matchMedia", workListDetailNarrowQuery)
		update := func() {
			layout := ListDetailWide
			if media.Get("matches").Bool() {
				layout = ListDetailNarrow
			}
			state.Set(layout)
		}
		update()
		listener := js.FuncOf(func(js.Value, []js.Value) any {
			update()
			return nil
		})
		if media.Get("addEventListener").Type() == js.TypeFunction {
			media.Call("addEventListener", "change", listener)
		} else {
			media.Call("addListener", listener)
		}
		return func() {
			if media.Get("removeEventListener").Type() == js.TypeFunction {
				media.Call("removeEventListener", "change", listener)
			} else if media.Get("removeListener").Type() == js.TypeFunction {
				media.Call("removeListener", listener)
			}
			listener.Release()
		}
	}, struct{}{})
}
