//go:build !(js && wasm)

package main

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// Issue 71: the arrival-order test takes its dates from the clock it runs on, and
// an answer that has genuinely expired is still left out.
func TestIssue71_ExpiredAnswerStaysHidden(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	live := personaChatAnswer{ID: "live", ThreadID: "q", Body: "Still here.", CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(23 * time.Hour)}
	gone := personaChatAnswer{ID: "gone", ThreadID: "q", Body: "Too late.", CreatedAt: now.Add(-48 * time.Hour), ExpiresAt: now.Add(-24 * time.Hour)}
	model := &chatui.Model{}
	if !chatbug040ApplyAnswers(model, []personaChatAnswer{live, gone}, now) {
		t.Fatal("the live answer was not applied")
	}
	if len(model.EphemeralMessages) != 1 || model.EphemeralMessages[0].ID != "live" {
		t.Fatalf("expired answer drawn: %+v", model.EphemeralMessages)
	}
}
