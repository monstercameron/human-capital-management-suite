//go:build js && wasm

package productui

import (
	"sync"
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// scrollPositions remembers the last known scrollTop for every restoration-
// enabled ScrollRegion instance, keyed by its element ID. It is intentionally
// process-wide and in-memory (not sessionStorage): the concern this fixes is
// a same-page re-render (sort, filter, or paginate a table; a route the
// reconciler re-mounts) replacing a scroll region's DOM node and silently
// resetting it to the top, not restoring a position across a full page load.
var scrollPositions = struct {
	mu     sync.Mutex
	values map[string]float64
}{values: map[string]float64{}}

// useScrollRestoration keeps one named scroll region's position stable
// across re-renders (UIPOLISH-004 REFACTOR). It is always called -- GWC
// requires a stable hook order per component instance -- and does nothing
// whenever enabled is false or id is blank.
func useScrollRestoration(id string, enabled bool) {
	ui.UseEffectOf(func() func() {
		if !scrollRestorationEnabled(id, enabled) {
			return nil
		}
		return bindScrollRestoration(id)
	}, struct {
		ID      string
		Enabled bool
	}{id, enabled})
}

// bindScrollRestoration restores id's last known scrollTop (if any is
// recorded) and starts recording future scroll positions for it, returning
// the cleanup UseEffectOf runs on unmount or dependency change. It is the
// testable core of useScrollRestoration: a pure binding step over the DOM
// that does not itself depend on being called from inside a component.
func bindScrollRestoration(id string) func() {
	doc := js.Global().Get("document")
	if !doc.Truthy() || doc.Get("getElementById").Type() != js.TypeFunction {
		return nil
	}
	element := doc.Call("getElementById", id)
	if !element.Truthy() {
		return nil
	}
	scrollPositions.mu.Lock()
	saved, ok := scrollPositions.values[id]
	scrollPositions.mu.Unlock()
	if ok {
		element.Set("scrollTop", saved)
	}
	listener := js.FuncOf(func(js.Value, []js.Value) any {
		scrollPositions.mu.Lock()
		scrollPositions.values[id] = element.Get("scrollTop").Float()
		scrollPositions.mu.Unlock()
		return nil
	})
	element.Call("addEventListener", "scroll", listener)
	return func() {
		element.Call("removeEventListener", "scroll", listener)
		listener.Release()
	}
}
