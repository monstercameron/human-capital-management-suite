package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func TestTodo_CHATBUG_006_FeedbackLedger(t *testing.T) {
	var ledger personaFeedbackLedger
	if ledger.rating("run") != "" {
		t.Fatal("unrated answer reports a rating")
	}
	ledger.confirm("run", personaFeedbackRating(true))
	ledger.confirm("other", personaFeedbackRating(false))
	if ledger.rating("run") != chatui.AgentFeedbackHelpful || ledger.rating("other") != chatui.AgentFeedbackNotRight {
		t.Fatalf("ratings: %q %q", ledger.rating("run"), ledger.rating("other"))
	}
	// A failed change leaves the confirmed rating untouched; an accepted undo clears it.
	ledger.confirm("run", "")
	if ledger.rating("run") != "" || ledger.rating("other") != chatui.AgentFeedbackNotRight {
		t.Fatalf("undo: %q %q", ledger.rating("run"), ledger.rating("other"))
	}

	original := map[string]string{"a": chatui.AgentFeedbackHelpful}
	restored := personaFeedbackRestoredWith(original, "b", "")
	if v, ok := restored["b"]; !ok || v != "" || restored["a"] != chatui.AgentFeedbackHelpful || len(original) != 1 {
		t.Fatalf("restored=%v original=%v", restored, original)
	}
	cleared := personaFeedbackRestoredWithout(restored, "b")
	if _, ok := cleared["b"]; ok || cleared["a"] != chatui.AgentFeedbackHelpful || len(restored) != 2 {
		t.Fatalf("cleared=%v restored=%v", cleared, restored)
	}
	if got := personaFeedbackRestoredWithout(nil, "x"); len(got) != 0 {
		t.Fatalf("nil overrides: %v", got)
	}
}
