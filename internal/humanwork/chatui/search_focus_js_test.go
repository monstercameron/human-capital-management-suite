//go:build js && wasm

package chatui

import (
	"syscall/js"
	"testing"
)

func TestFocusSearchMessageUsesBoundBrowserMethodsAndFocusesTarget(t *testing.T) {
	global := js.Global()
	oldDocument, oldTimeout, oldFrame := global.Get("document"), global.Get("setTimeout"), global.Get("requestAnimationFrame")
	defer func() {
		global.Set("document", oldDocument)
		global.Set("setTimeout", oldTimeout)
		global.Set("requestAnimationFrame", oldFrame)
	}()
	state := global.Get("Object").New()
	factory := global.Get("Function").New("state", `return {querySelectorAll: function(selector) {
  state.selector = selector;
  const row = { dataset: { messageId: "post-7" }, isConnected: true,
    scrollIntoView: function(options) { state.block = options.block; },
    focus: function(options) { state.preventScroll = options.preventScroll; state.focused = true; },
    classList: { add: function(name) { state.added = name; }, remove: function(name) { state.removed = name; } }
  };
  return [row];
}}`)
	global.Set("document", factory.Invoke(state))
	timeoutFactory := global.Get("Function").New("state", `return function(callback) { state.timer = callback; return 1; }`)
	global.Set("setTimeout", timeoutFactory.Invoke(state))
	frameFactory := global.Get("Function").New("state", `return function(callback) { state.frameBound = this === globalThis; callback(0); }`)
	global.Set("requestAnimationFrame", frameFactory.Invoke(state))

	FocusSearchMessage("post-7")
	if !state.Get("frameBound").Bool() {
		t.Fatal("requestAnimationFrame was not called with its window receiver")
	}
	if got := state.Get("selector").String(); got != "[data-message-id]" {
		t.Fatalf("selector = %q", got)
	}
	if !state.Get("focused").Bool() || !state.Get("preventScroll").Bool() {
		t.Fatal("target article did not receive focus with scroll preservation")
	}
	if got := state.Get("block").String(); got != "center" {
		t.Fatalf("scroll block = %q", got)
	}
	if got := state.Get("added").String(); got != "search-target" {
		t.Fatalf("highlight class = %q", got)
	}
	timer := state.Get("timer")
	if !timer.Truthy() {
		t.Fatal("temporary highlight timer was not scheduled")
	}
}
