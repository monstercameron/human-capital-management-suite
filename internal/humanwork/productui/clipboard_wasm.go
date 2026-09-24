//go:build js && wasm

package productui

import (
	"errors"
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

var errClipboardUnavailable = errors.New("clipboard unavailable")

// copyToClipboard writes value to the browser clipboard via the standard
// asynchronous Clipboard API. It never blocks the render and it never
// echoes value anywhere else: the only other place this identifier appears
// is the redacted, masked text TechnicalDetails already drew on screen.
//
// done, when given, hears how the write went: nil once the browser accepted
// it, an error when the clipboard is missing or the write was refused (an
// insecure origin, an embedded frame, a denied permission). A refused write
// is handled here either way, so it never surfaces as an uncaught rejection.
func copyToClipboard(value string, done ...func(error)) {
	report := func(err error) {
		for _, callback := range done {
			if callback != nil {
				callback(err)
			}
		}
	}
	defer func() {
		if recover() != nil {
			report(errClipboardUnavailable)
		}
	}()
	navigator := js.Global().Get("navigator")
	if !navigator.Truthy() {
		report(errClipboardUnavailable)
		return
	}
	clipboard := navigator.Get("clipboard")
	if !clipboard.Truthy() || clipboard.Get("writeText").Type() != js.TypeFunction {
		report(errClipboardUnavailable)
		return
	}
	promise := clipboard.Call("writeText", value)
	var resolved, rejected js.Func
	settle := func(err error) {
		resolved.Release()
		rejected.Release()
		ui.PostAsync(func() { report(err) })
	}
	resolved = js.FuncOf(func(js.Value, []js.Value) any {
		settle(nil)
		return nil
	})
	rejected = js.FuncOf(func(_ js.Value, args []js.Value) any {
		err := errClipboardUnavailable
		if len(args) > 0 && args[0].Truthy() && args[0].Get("message").Type() == js.TypeString {
			err = errors.New(args[0].Get("message").String())
		}
		settle(err)
		return nil
	})
	promise.Call("then", resolved, rejected)
}
