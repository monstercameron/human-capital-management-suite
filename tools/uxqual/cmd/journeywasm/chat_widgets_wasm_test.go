//go:build js && wasm

package main

import (
	"syscall/js"
	"testing"
)

func TestChannelMilestoneFormClearAfterSavedAdd(t *testing.T) {
	global := js.Global()
	previous := global.Get("document")
	defer global.Set("document", previous)
	fields := map[string]js.Value{}
	for id, value := range map[string]string{"channel-milestone-new": "QA milestone", "channel-milestone-new-status": "DONE", "channel-milestone-new-owner": "guest.owner", "channel-milestone-new-date": "2026-10-01"} {
		el := global.Get("Object").New()
		el.Set("value", value)
		fields[id] = el
	}
	get := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return js.Null()
		}
		if el, ok := fields[args[0].String()]; ok {
			return el
		}
		return js.Null()
	})
	defer get.Release()
	doc := global.Get("Object").New()
	doc.Set("getElementById", get)
	global.Set("document", doc)
	clearChannelMilestoneForm()
	for id, want := range map[string]string{"channel-milestone-new": "", "channel-milestone-new-status": "PLANNED", "channel-milestone-new-owner": "", "channel-milestone-new-date": ""} {
		if got := fields[id].Get("value").String(); got != want {
			t.Errorf("%s=%q want %q", id, got, want)
		}
	}
}
