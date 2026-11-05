package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATREVIEW-001: below 390px the operations tabs hide the move tab's long
// label (display:none) and show the short one instead. The short label must
// stay in the accessible tree, or the tab has no accessible name at narrow
// widths: the long label is display:none (removed from the tree) and the
// short label was aria-hidden.
func TestChatReview001_MoveTabShortLabelHasAccessibleName(t *testing.T) {
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		view := NewView(PageAgentOperations, "tenant", "admin", "")
		view.Locale = ResolveProductLocale(localeID)
		markup, err := ui.RenderToString(agentOperationsTabs(view, view.Locale))
		if err != nil {
			t.Fatal(err)
		}
		short := agentOperationsText(view.Locale, "tab_portable_short")
		if !strings.Contains(markup, short) {
			t.Fatalf("%s short move-tab label missing: %s", localeID, markup)
		}
		at := strings.Index(markup, "agent-operations-tab-label-short")
		if at < 0 {
			t.Fatalf("%s short-label span missing: %s", localeID, markup)
		}
		open := strings.LastIndex(markup[:at], "<span")
		end := strings.Index(markup[at:], ">")
		if open < 0 || end < 0 {
			t.Fatalf("%s cannot isolate short-label span: %s", localeID, markup)
		}
		if tag := markup[open : at+end+1]; strings.Contains(tag, "aria-hidden") {
			t.Fatalf("%s short move-tab label is hidden from assistive tech: %s", localeID, tag)
		}
	}
}
