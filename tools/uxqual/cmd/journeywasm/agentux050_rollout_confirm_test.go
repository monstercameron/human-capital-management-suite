package main

import (
	"os"
	"strings"
	"testing"
)

// TestTodo_AGENTUX_050_RolloutConfirm: a rollout step asks for its confirmation
// on the page, not in the browser's own dialog. The page code is wasm-only, so
// the test reads its source: the rollout handlers go through
// rolloutConfirmedInPage, and no call to the browser's confirm remains in them.
func TestTodo_AGENTUX_050_RolloutConfirm(t *testing.T) {
	raw, err := os.ReadFile("agent_rollout_portable_wasm.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	if strings.Contains(source, `Call("confirm"`) {
		t.Error("a rollout step still opens the browser's confirm dialog")
	}
	if got := strings.Count(source, "rolloutConfirmedInPage("); got != 2 {
		t.Errorf("%d rollout handlers ask for confirmation on the page, want the button and the form", got)
	}
	helper, err := os.ReadFile("agentux050_rollout_confirm_wasm.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"data-confirming", "true"`, `"blur"`, `"confirmLabel"`, "func rolloutDisarm("} {
		if !strings.Contains(string(helper), want) {
			t.Errorf("the in-page confirmation lacks %s", want)
		}
	}
}
