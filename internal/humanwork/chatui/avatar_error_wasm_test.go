//go:build js && wasm

package chatui

import (
	"syscall/js"
	"testing"
)

func TestChatPhotoFailureThenLoadRestoresPhoto(t *testing.T) {
	target := js.Global().Get("Object").New()
	style := js.Global().Get("Object").New()
	target.Set("style", style)
	setChatPhotoVisible(target, false)
	if got := style.Get("display").String(); got != "none" {
		t.Fatalf("broken photo display = %q", got)
	}
	setChatPhotoVisible(target, true)
	if got := style.Get("display").String(); got != "" {
		t.Fatalf("recovered photo display = %q", got)
	}
}

// Issue 3827: the error handler remembers the address that failed, so the
// next row drawn for that author leaves the portrait out.
func TestIssue3827PhotoErrorIsRememberedByAddress(t *testing.T) {
	const address = "/workspace/assets/issue3827-wasm-missing.jpg"
	t.Cleanup(func() { chatPhotoResolved(address, true) })
	target := js.Global().Get("Object").New()
	target.Set("getAttribute", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 1 && args[0].String() == "src" {
			return address
		}
		return nil
	}))
	rememberChatPhoto(target, true)
	if chatPhotoURL(address) != address {
		t.Fatal("a portrait that loaded is withheld")
	}
	rememberChatPhoto(target, false)
	if got := chatPhotoURL(address); got != "" {
		t.Fatalf("a portrait that failed is drawn again: %q", got)
	}
	rememberChatPhoto(js.Undefined(), false)
}
