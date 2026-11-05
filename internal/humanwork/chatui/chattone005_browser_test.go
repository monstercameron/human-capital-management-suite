package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
)

// TestTodo_CHATTONE_005_Browser: what a reader who may not view messages as
// written sees while a rewording is still produced, and what a direct message
// looks like. In every language the pending message carries the "not ready"
// stand-in (never the original, never nothing, with no "Show as written" to
// press); a direct message is delivered as written with no reword mark.
func TestTodo_CHATTONE_005_Browser(t *testing.T) {
	pendingWords := map[string]struct{ standin, show string }{
		"en-US": {RenderingText("en-US", "pending"), "Show as written"},
		"de-DE": {RenderingText("de-DE", "pending"), "Wie geschrieben anzeigen"},
		"ar":    {RenderingText("ar", "pending"), "عرض النص كما كُتب"},
	}
	for locale, want := range pendingWords {
		if want.standin == "" {
			t.Fatalf("%s: no pending stand-in copy", locale)
		}
		m, msg := w10Model(locale, "eve")
		m.ReaderSelections["p1"] = ReaderSelection{Revision: 1,
			Mark: chatrender.Mark{State: "pending", CanShowOriginal: false, CanShowAsWritten: false, Wanted: []chatrender.Kind{chatrender.Reword}}}
		got := chatlangMessageMarkup(t, m, msg, localUI{})
		if !strings.Contains(got, want.standin) {
			t.Errorf("%s: the pending message has no stand-in: %s", locale, got)
		}
		if strings.Contains(got, w10Original) {
			t.Errorf("%s: the original is shown while the rewording is produced", locale)
		}
		if strings.Contains(got, want.show) {
			t.Errorf("%s: originals are hidden but Show as written is offered", locale)
		}
		if body := ReaderMessageBody(m, msg); body == "" || strings.Contains(body, "idiot") {
			t.Errorf("%s: the pending body reads %q", locale, body)
		}
	}

	// A direct message is delivered as written: the whole body, no reword
	// mark, no pending stand-in, no writer's "Others see".
	m, msg := w10Model("en-US", "eve")
	got := chatlangMessageMarkup(t, m, msg, localUI{})
	if !strings.Contains(got, w10Original) {
		t.Errorf("a direct message is not delivered as written: %s", got)
	}
	for _, mark := range []string{`data-chatlang-state="reworded"`, "chattone-others", RenderingText("en-US", "pending")} {
		if strings.Contains(got, mark) {
			t.Errorf("a direct message carries a rewording mark: %q", mark)
		}
	}
}
