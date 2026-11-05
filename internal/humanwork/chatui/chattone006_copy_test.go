package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
)

// TestTodo_CHATTONE_006_Copy: the status line can name the stall and the
// taking-longer escalation. The toolbar carries both as data-copy attributes
// in every language, so the wasm controller's "stalled" and "slow" codes
// resolve to words instead of falling back to the general unavailable.
func TestTodo_CHATTONE_006_Copy(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, key := range []string{"stalled", "slow"} {
			if ChattoneText(locale, key) == "" {
				t.Fatalf("missing copy %s %s", locale, key)
			}
		}
		p := ChattoneToolbarProps{Locale: locale, Target: "chat-composer", Conversation: "room", Draft: "one two three", Styles: chatrewrite.DefaultStyles(), Enabled: true}
		markup, err := ui.RenderToString(RenderChattoneToolbar(p))
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"stalled", "slow"} {
			if !strings.Contains(markup, `data-copy-`+key+`=`) {
				t.Fatalf("%s toolbar has no data-copy-%s in %s", locale, key, markup)
			}
			if !strings.Contains(markup, ChattoneText(locale, key)) {
				t.Fatalf("%s toolbar does not carry its %s words", locale, key)
			}
		}
		if locale == "ar" && !strings.Contains(markup, `dir="rtl"`) {
			t.Fatal("missing RTL")
		}
	}
	// The stalled line names the retry; the slow line keeps the working
	// reassurance that typing on is safe.
	if stalled := ChattoneText("en-US", "stalled"); !strings.Contains(stalled, "did not answer") || !strings.Contains(stalled, "Try") {
		t.Fatalf("the stalled line does not name the retry: %q", stalled)
	}
	if slow := ChattoneText("en-US", "slow"); !strings.Contains(slow, "longer than usual") || !strings.Contains(slow, "Keep typing") {
		t.Fatalf("the slow line does not escalate the wait: %q", slow)
	}
}
