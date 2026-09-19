package productui

import "testing"

// TestTodo_UXLIVE_016_Browser records the one row this todo deliberately did
// not change, so the decision is visible rather than forgotten: workflow
// History keeps the stored ISO effective date for the default locale,
// because UIPOLISH-007 fixed that form and the alternative this package's
// localizer produces for en-US is the ambiguous "12/01/2026", which would be
// a third date vocabulary rather than one fewer.
//
// The instants themselves are unified in tools/uxqual/journeyclient, where
// they are formatted; see TestTodo_UXLIVE_016 there.
func TestTodo_UXLIVE_016_Browser(t *testing.T) {
	if got := historyEffectiveDateLabel(ResolveProductLocale("en-US"), "2026-06-01"); got != "2026-06-01" {
		t.Fatalf("the en-US history effective date changed to %q; UIPOLISH-007 fixed this form", got)
	}
	if got := historyEffectiveDateLabel(ResolveProductLocale("de-DE"), "2026-06-01"); got == "2026-06-01" {
		t.Fatalf("the de-DE history effective date is no longer localized: %q", got)
	}
}
