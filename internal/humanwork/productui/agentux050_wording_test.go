package productui

import (
	"strings"
	"testing"
)

// TestTodo_AGENTUX_050: the staged rollout reads "Approve", "Update the first
// conversation", "Looks right, update the rest" and the done sentence with the
// version, in English, German and Arabic, with no internal object ("rollout for
// ...") appended to a step. The emoji half of the entry is
// TestTodo_AGENTUX_050 in internal/humanwork/chatui, and the in-page
// confirmation is TestTodo_AGENTUX_050_Browser in tools/uxqual/cmd/journeywasm.
func TestTodo_AGENTUX_050(t *testing.T) {
	plan := AgentRolloutPlan{CanaryCount: 1, Candidates: make([]AgentRolloutCandidate, 4)}
	steps := []struct {
		name, action string
		progress     *AgentRolloutProgress
		want         map[string]string
	}{
		{"approve", "APPROVE", nil, map[string]string{"en-US": "Approve", "de-DE": "Genehmigen", "ar": "الموافقة"}},
		{"first", "ADVANCE", &AgentRolloutProgress{Stage: "APPROVED", Cursor: 0}, map[string]string{"en-US": "Update the first conversation", "de-DE": "Erste Unterhaltung aktualisieren", "ar": "تحديث المحادثة الأولى"}},
		{"rest", "PROMOTE", &AgentRolloutProgress{Stage: "CANARY_COMPLETE", Cursor: 1}, map[string]string{"en-US": "Looks right, update the rest", "de-DE": "Sieht richtig aus, übrige aktualisieren", "ar": "تبدو سليمة، حدّث البقية"}},
	}
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		for _, step := range steps {
			got := agentRolloutActionLabel(locale, step.action, "rollout for Policy Helper", step.progress, plan)
			if got != step.want[language] {
				t.Errorf("%s %s: button reads %q, want %q", language, step.name, got, step.want[language])
			}
			if strings.Contains(got, "Policy Helper") || strings.Contains(strings.ToLower(got), "rollout") {
				t.Errorf("%s %s: %q names the internal object", language, step.name, got)
			}
		}
		if done := agentRolloutStageFor(locale, "COMPLETE", 6); strings.Contains(done, "{") || done == agentRolloutStage(locale, "COMPLETE") {
			t.Errorf("%s: the last step says %q", language, done)
		}
	}
	if got := agentRolloutStageFor(ResolveProductLocale("en-US"), "COMPLETE", 6); got != "Done: every selected conversation runs version 6." {
		t.Errorf("done sentence = %q", got)
	}
}
