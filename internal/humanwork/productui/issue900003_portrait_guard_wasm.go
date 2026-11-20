//go:build js && wasm

package productui

import "syscall/js"

var portraitGuardInstalled bool

// InstallPortraitGuard replaces every avatar portrait that has failed, or
// fails later, with an initials badge. It handles an image that already
// finished failing before this client started (complete with a natural width
// of zero) and listens in the capture phase for failures afterwards, since an
// image error does not bubble. It is safe to call more than once.
func InstallPortraitGuard() {
	document := js.Global().Get("document")
	if !document.Truthy() {
		return
	}
	sweep := func() {
		images := document.Call("querySelectorAll", PortraitGuardSelector)
		for i := 0; i < images.Get("length").Int(); i++ {
			img := images.Index(i)
			if img.Get("complete").Bool() && img.Get("naturalWidth").Int() == 0 {
				replaceFailedPortrait(document, img)
			}
		}
	}
	sweep()
	if portraitGuardInstalled {
		return
	}
	portraitGuardInstalled = true
	onError := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		if target := args[0].Get("target"); target.Truthy() && target.Call("matches", PortraitGuardSelector).Bool() {
			replaceFailedPortrait(document, target)
		}
		return nil
	})
	document.Call("addEventListener", "error", onError, true)
	onLoad := js.FuncOf(func(js.Value, []js.Value) any { sweep(); return nil })
	js.Global().Call("addEventListener", "load", onLoad)
}

func replaceFailedPortrait(document, img js.Value) {
	if !img.Get("parentNode").Truthy() {
		return
	}
	label := img.Call("getAttribute", portraitInitialsAttr)
	if label.Type() != js.TypeString || label.String() == "" {
		return
	}
	// Record the failure so the client's own render of this avatar (the
	// hydration that follows the first sweep, or any later render) draws the
	// same initials badge instead of an <img> that no longer matches the page.
	if src := img.Call("getAttribute", "src"); src.Type() == js.TypeString {
		markPortraitBroken(src.String())
	}
	badge := document.Call("createElement", "span")
	badge.Set("className", img.Get("className").String())
	badge.Set("textContent", label.String())
	badge.Call("setAttribute", "aria-hidden", "true")
	img.Call("replaceWith", badge)
}
