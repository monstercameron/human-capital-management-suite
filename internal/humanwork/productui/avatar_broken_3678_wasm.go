//go:build js && wasm

package productui

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// useBrokenPortraitSweep finds the portrait <img> a server render put in the
// page and reports it broken when it already failed (complete with no pixels)
// or fails later. The server-rendered image can fail before the client bound
// its onerror, so the handler on the element alone is not enough (issue 3678).
func useBrokenPortraitSweep(photoURL string, onBroken func()) {
	ui.UseEffectOf(func() func() {
		if photoURL == "" {
			return nil
		}
		images := js.Global().Get("document").Get("images")
		for i := 0; i < images.Get("length").Int(); i++ {
			img := images.Index(i)
			src := img.Call("getAttribute", "src")
			if src.Type() != js.TypeString || src.String() != photoURL {
				continue
			}
			if img.Get("complete").Bool() {
				if img.Get("naturalWidth").Int() == 0 {
					markPortraitBroken(photoURL)
					onBroken()
				}
				continue
			}
			var fn js.Func
			fn = js.FuncOf(func(js.Value, []js.Value) any {
				markPortraitBroken(photoURL)
				onBroken()
				return nil
			})
			img.Call("addEventListener", "error", fn, map[string]any{"once": true})
		}
		return nil
	}, photoURL)
}
