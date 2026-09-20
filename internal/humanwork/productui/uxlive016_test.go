package productui

import "testing"

// TestTodo_UXLIVE_016_Browser: workflow History's effective date, the one row
// UXLIVE-016 left as the stored ISO key because the only English alternative
// was the ambiguous "06/01/2026", now reads like every other product date.
//
// The instants themselves are unified in tools/uxqual/journeyclient, where
// they are formatted; see TestTodo_UXLIVE_016 there.
func TestTodo_UXLIVE_016_Browser(t *testing.T) {
	if got := historyEffectiveDateLabel(ResolveProductLocale("en-US"), "2026-06-01"); got != "1 Jun 2026" {
		t.Fatalf("the en-US history effective date = %q, want the product form 1 Jun 2026", got)
	}
	if got := historyEffectiveDateLabel(ResolveProductLocale("de-DE"), "2026-06-01"); got == "2026-06-01" {
		t.Fatalf("the de-DE history effective date is no longer localized: %q", got)
	}
}
