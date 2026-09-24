//go:build js && wasm

package chatui

import (
	"syscall/js"
	"testing"
)

func TestPaneResizeEventsStayWithCapturedPointer(t *testing.T) {
	if !samePanePointer(js.ValueOf(7), js.ValueOf(7)) {
		t.Fatal("the captured pointer was rejected")
	}
	if samePanePointer(js.ValueOf(7), js.ValueOf(8)) {
		t.Fatal("a second pointer was accepted as the active drag")
	}
	if samePanePointer(js.Undefined(), js.ValueOf(7)) || samePanePointer(js.ValueOf(7), js.Undefined()) {
		t.Fatal("an event without a numeric pointer ID was accepted")
	}
}
