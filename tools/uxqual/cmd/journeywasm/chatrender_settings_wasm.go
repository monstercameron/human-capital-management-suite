//go:build js && wasm

package main

import (
	"net/url"
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// renderingSettingsSubmit reads the actual form at submit. Save is injected by
// the composition owner and uses the same authenticated HTTP session as chat.
func renderingSettingsSubmit(locale string, pref chatrender.Preference, conversation string, save func(string, chatrender.Preference), failed func(string)) ui.Handler {
	return ui.UseEvent(func(event ui.Event) {
		event.PreventDefault()
		form := js.Global().Get("document").Call("querySelector", ".chatrender-settings")
		if !form.Truthy() {
			if failed != nil {
				failed(chatui.RenderingText(locale, "error"))
			}
			return
		}
		values := url.Values{}
		values.Set("reading", form.Call("querySelector", "[name=reading]").Get("value").String())
		if form.Call("querySelector", "[name=translate]").Get("checked").Bool() {
			values.Set("translate", "on")
		}
		for _, name := range []string{"further", "never"} {
			nodes := form.Call("querySelectorAll", "[name="+name+"]:checked")
			for i := 0; i < nodes.Length(); i++ {
				values.Add(name, nodes.Index(i).Get("value").String())
			}
		}
		next, err := renderingSettingsFromForm(values, pref)
		if err != nil {
			if failed != nil {
				failed(chatui.RenderingText(locale, "error"))
			}
			return
		}
		room := form.Call("querySelector", "[name=conversation]")
		if !room.Truthy() || !room.Get("checked").Bool() {
			conversation = ""
		}
		if save != nil {
			save(renderingSettingsURL(conversation), next)
		}
	})
}
