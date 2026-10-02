package main

import (
	"os"
	"strings"
	"testing"
)

// TestTodo_AGENTUX_050_Browser: no page of Chat or Agents opens the browser's
// confirm or prompt dialog. The three places that still did (the owner
// controls, the agent administration commands and the editor's Discard) ask on
// the page, in the button itself, and a destructive one takes the danger style.
// The page code is wasm-only, so the test reads its source; the rollout half is
// TestTodo_AGENTUX_050_RolloutConfirm.
func TestTodo_AGENTUX_050_Browser(t *testing.T) {
	read := func(name string) string {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	for name, helper := range map[string]string{
		"agent_controls_wasm.go":            "inPageConfirmed(confirmation.String(), button, ownerActionDestructive(",
		"persona_admin_commands_wasm.go":    "inPageConfirmed(question, button, personaCommandDestructive(",
		"agentux039_editor_close_wasm.go":   "inPageConfirmed(domDataset(form, \"personaEditorDiscard\"), cancel, true)",
		"agent_rollout_portable_wasm.go":    "rolloutConfirmedInPage(",
		"agentux050_inpage_confirm_wasm.go": "func inPageConfirmed(",
	} {
		source := read(name)
		for _, native := range []string{`Call("confirm"`, `Call("prompt"`, `Call("alert"`} {
			if strings.Contains(source, native) {
				t.Errorf("%s still opens the browser's dialog %s", name, native)
			}
		}
		if !strings.Contains(source, helper) {
			t.Errorf("%s lacks %s", name, helper)
		}
	}
	if !strings.Contains(read("persona_admin_commands_wasm.go"), "inPageReason(wrapper, button,") {
		t.Error("the rejection reason is still asked in a browser prompt")
	}
	// The disarm puts the label and the style back.
	for _, want := range []string{`"confirmDanger"`, `"confirmClass"`} {
		if !strings.Contains(read("agentux050_rollout_confirm_wasm.go"), want) {
			t.Errorf("putting the label back does not also put back %s", want)
		}
	}
	for _, destroys := range []string{"stop", "quarantine", "delete", "revoke", "retire"} {
		if !ownerActionDestructive(destroys) {
			t.Errorf("%s is not styled as destructive", destroys)
		}
	}
	for _, keeps := range []string{"pause", "resume", "export", "dry_run", "publish", "hold"} {
		if ownerActionDestructive(keeps) {
			t.Errorf("%s is styled as destructive", keeps)
		}
	}
	for _, destroys := range []string{"RETIRE", "retire", "REJECT", "SUSPEND"} {
		if !personaCommandDestructive(destroys) {
			t.Errorf("%s is not styled as destructive", destroys)
		}
	}
	if personaCommandDestructive("REVIEW") || personaCommandDestructive("CREATE_VERSION") {
		t.Error("a non-destructive command is styled as destructive")
	}
}
