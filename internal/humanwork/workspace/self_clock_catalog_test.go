package workspace

import "testing"

func TestTranslateSelfClockCatalogSupportsReviewedLocales(t *testing.T) {
	for _, locale := range SupportedLocales() {
		ctx := ResolveLocale(locale)
		for _, status := range []string{"CLOCKED_IN", "CLOCKED_OUT", "ON_BREAK"} {
			if got := TranslateSelfClockStatus(ctx, status); got == "" || got[0] == '[' {
				t.Fatalf("locale=%s status=%s got=%q", locale, status, got)
			}
		}
		if got := TranslateSelfClockLastEvent(ctx, ""); got == "" || got[0] == '[' {
			t.Fatalf("locale=%s no-event=%q", locale, got)
		}
		if got := TranslateSelfClockLastEvent(ctx, "2026-09-28T12:00:00Z"); got != "2026-09-28T12:00:00Z" {
			t.Fatalf("locale=%s event=%q", locale, got)
		}
	}
}
