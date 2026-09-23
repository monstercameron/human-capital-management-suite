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
