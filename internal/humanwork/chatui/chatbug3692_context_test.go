package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// TestIssue_3692_ContextMarker: the conversation folded under a moderation item
// is marked, so the type-to-filter on the page can leave it out of the text it
// matches. Without the marker, a word that only sits in the surrounding messages
// (not the item's own text) keeps a card that shows no match on screen.
func TestIssue_3692_ContextMarker(t *testing.T) {
	at := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	item := chat.ModerationItem{
		ID: "filter:7", Kind: "filter", State: "OPEN", PostID: "m2", ConversationID: "room", ConversationName: "benefits",
		Message: chat.Post{ID: "m2", Body: "call the benefits portal at 555-0100", Sequence: 2, CreatedAt: at},
		Reason:  "personal_data", At: at,
		Context: []chat.Post{
			{ID: "m1", Body: "here is the gift card code", Sequence: 1, CreatedAt: at},
			{ID: "m2", Body: "call the benefits portal at 555-0100", Sequence: 2, CreatedAt: at},
		},
	}
	markup := chatremoveMarkup(t, ModerationPage(ModerationPageModel{Locale: "en-US", State: StateReady, Tab: "open", Items: []chat.ModerationItem{item}}))
	start := strings.Index(markup, `<details class="chatmod005-context"`)
	if start < 0 {
		t.Fatalf("the conversation around the item is not folded in a details element: %s", markup)
	}
	tag := markup[start:]
	tag = tag[:strings.Index(tag, ">")+1]
	if !strings.Contains(tag, `data-chatremove-context="true"`) {
		t.Fatalf("the folded conversation carries no marker for the page filter: %s", tag)
	}
	if !strings.Contains(markup, "here is the gift card code") {
		t.Fatalf("the surrounding message is not rendered in the folded block: %s", markup)
	}
}
