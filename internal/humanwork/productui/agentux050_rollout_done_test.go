package productui

import (
	"strings"
	"testing"
)

// TestTodo_AGENTUX_050_RolloutDone: the last step of a staged rollout says what
// happened, with the version, in each language; every earlier stage keeps the
// sentence it had.
func TestTodo_AGENTUX_050_RolloutDone(t *testing.T) {
	want := map[string]string{
		"en-US": "Done: every selected conversation runs version 6.",
		"de-DE": "Fertig: Jede ausgewählte Unterhaltung nutzt jetzt Version 6.",
	}
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		got := agentRolloutStageFor(locale, "COMPLETE", 6)
		if strings.Contains(got, "{version}") || got == agentRolloutStage(locale, "COMPLETE") {
			t.Errorf("%s: the last step says %q", language, got)
		}
		if sentence, ok := want[language]; ok && got != sentence {
			t.Errorf("%s: got %q, want %q", language, got, sentence)
		}
		for _, stage := range []string{"PREVIEWED", "APPROVED", "CANARY_COMPLETE"} {
			if agentRolloutStageFor(locale, stage, 6) != agentRolloutStage(locale, stage) {
				t.Errorf("%s: stage %s lost its sentence", language, stage)
			}
		}
		// With no version to name, the generic line stands.
		if agentRolloutStageFor(locale, "COMPLETE", 0) != agentRolloutStage(locale, "COMPLETE") {
			t.Errorf("%s: a rollout with no version names one", language)
		}
	}
}
