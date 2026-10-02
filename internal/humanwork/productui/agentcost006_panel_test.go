package productui

import "testing"

func TestAgentCostParsing(t *testing.T) {
	for text, want := range map[string]int64{"": 0, "5": 5_000_000, "$12.50": 12_500_000, " 0.07 ": 70_000} {
		if got, ok := parseAgentCostMicros(text); !ok || got != want {
			t.Errorf("parseAgentCostMicros(%q) = %d, %v, want %d", text, got, ok, want)
		}
	}
	for _, text := range []string{"-1", "abc", "1e99", "NaN", "5 dollars"} {
		if _, ok := parseAgentCostMicros(text); ok {
			t.Errorf("parseAgentCostMicros(%q) accepted", text)
		}
	}
	for text, want := range map[string]int64{"": 0, "20": 20, " 3 ": 3} {
		if got, ok := parseAgentCostCount(text); !ok || got != want {
			t.Errorf("parseAgentCostCount(%q) = %d, %v", text, got, ok)
		}
	}
	for _, text := range []string{"-2", "1.5", "x"} {
		if _, ok := parseAgentCostCount(text); ok {
			t.Errorf("parseAgentCostCount(%q) accepted", text)
		}
	}
}

func TestAgentTierLabel(t *testing.T) {
	en, de := ResolveProductLocale("en-US"), ResolveProductLocale("de-DE")
	if got := agentTierLabel(en, "T3_SUBMIT_GOVERNED"); got != "Submit for approval" {
		t.Errorf("T3 label = %q", got)
	}
	if got := agentTierLabel(de, "T0"); got != "Informationen lesen" {
		t.Errorf("T0 de label = %q", got)
	}
	if got := agentTierLabel(en, "unknown"); got != "unknown" {
		t.Errorf("an unknown tier should print as given, got %q", got)
	}
}
