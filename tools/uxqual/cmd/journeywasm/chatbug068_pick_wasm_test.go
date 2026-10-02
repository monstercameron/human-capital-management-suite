//go:build js && wasm

package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// CHATBUG-068 (mention pick). Writing a picked name into the draft only stores
// it, and a stored draft schedules no render, so the page callback the menu
// calls after a pick must ask the page to draw: that is what puts the note
// under the composer when the person picked someone outside the conversation.
func TestTodo_CHATBUG_068_MentionPickSchedulesARender(t *testing.T) {
	oldRerender, oldRetry := chatRerender, productRouteRetry
	t.Cleanup(func() { chatRerender, productRouteRetry = oldRerender, oldRetry })
	renders := 0
	chatRerender = func() { renders++ }
	productRouteRetry = nil

	callbacks := chatCallbacks(journeyclient.Config{Tenant: "tenant", Subject: "reader", Locale: "en-US"})
	if callbacks.MentionPicked == nil {
		t.Fatal("the page does not hear that a mention was picked")
	}
	callbacks.MentionPicked()
	if renders != 1 {
		t.Fatalf("a pick scheduled %d local renders, want 1", renders)
	}
}
