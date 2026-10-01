//go:build js && wasm

package main

import (
	"strings"
	"syscall/js"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestTodo_AGENT_043_ControlsWASM(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		mount := js.Global().Call("eval", `({isConnected:true,innerHTML:"",getAttribute:function(){return this.locale}})`)
		mount.Set("locale", locale)
		renderAgentControlsMount(mount, productui.AgentControlsSnapshot{Available: true, CanDraft: true}, "done")
		markup := mount.Get("innerHTML").String()
		if !strings.Contains(markup, `id="agent-schedule-zone"`) || !strings.Contains(markup, `aria-describedby="agent-controls-status"`) || strings.Contains(markup, `data-owner-action="pause"`) {
			t.Fatalf("%s WASM projection widened authority or lost field semantics", locale)
		}
		if locale == "ar" && !strings.Contains(markup, `dir="rtl"`) {
			t.Fatal("WASM lost RTL direction")
		}
	}
}

func TestTodo_AGENT_043_ControlsWASMValidation(t *testing.T) {
	previous := js.Global().Get("document")
	document := js.Global().Call("eval", `({field:{invalid:false,focused:false,setAttribute:function(k,v){this.invalid=v},focus:function(){this.focused=true}},status:{dataset:{msgInvalid:"Invalid whole number"}},getElementById:function(id){return id==="agent-controls-status"?this.status:this.field}})`)
	js.Global().Set("document", document)
	t.Cleanup(func() { js.Global().Set("document", previous) })
	reportAgentControlsInputError("agent-schedule-revision")
	if document.Get("field").Get("invalid").String() != "true" || !document.Get("field").Get("focused").Bool() || document.Get("status").Get("textContent").String() != "Invalid whole number" {
		t.Fatal("invalid revision did not focus and announce its associated error")
	}
}
