package main

import "testing"

func TestGeneralAgentSelectionNeverSendsABrowserDefault(t *testing.T) {
	for _, value := range []string{"", " ", "on", "general-agent", "<null>", "<undefined>"} {
		if got := generalAgentSelection(value); got != "" {
			t.Fatalf("selection %q was sent as agent id %q", value, got)
		}
	}
	if got := generalAgentSelection(" hcmnext.local.persona.policy_helper "); got != "hcmnext.local.persona.policy_helper" {
		t.Fatalf("named agent id = %q", got)
	}
}
