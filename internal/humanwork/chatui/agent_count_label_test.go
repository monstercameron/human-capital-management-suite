package chatui

import "testing"

func TestAgentCountLabelSingular(t *testing.T) {
	for locale, want := range map[string]string{"en-US": "1 agent", "de-DE": "1 Agent", "ar": "وكيل واحد"} {
		m := Model{Locale: locale}
		if got := agentCountLabel(m, 1); got != want {
			t.Fatalf("%s: one agent reads %q, want %q", locale, got, want)
		}
	}
	if got := agentCountLabel(Model{Locale: "en-US"}, 2); got != "2 agents" {
		t.Fatalf("two agents read %q", got)
	}
}
