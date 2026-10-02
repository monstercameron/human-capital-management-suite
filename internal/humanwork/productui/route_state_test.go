package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestTodo_CHATBUG_024_RouteStates: an address no page owns, and a page whose
// read failed, each show an ordinary state in the reader's language. Neither
// prints the error behind it, and the failed read keeps its denied-journey
// message only when the caller passes reviewed copy for it.
func TestTodo_CHATBUG_024_RouteStates(t *testing.T) {
	for locale, words := range map[string][5]string{
		"en-US": {"This page does not exist", "Check the address", "Home", "This page could not be loaded", "Try again"},
		"de-DE": {"Diese Seite existiert nicht", "Prüfen Sie die Adresse", "Startseite", "Diese Seite konnte nicht geladen werden", "Erneut versuchen"},
		"ar":    {"هذه الصفحة غير موجودة", "تحقق من العنوان", "الرئيسية", "تعذر تحميل هذه الصفحة", "حاول مرة أخرى"},
	} {
		ctx := ResolveProductLocale(locale)
		missing, err := ui.RenderToString(RouteNotFound(ctx))
		if err != nil {
			t.Fatal(err)
		}
		failed, err := ui.RenderToString(RouteLoadFailed(ctx, func() {}, ""))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{words[0], words[1], words[2], `href="` + Path(PageHome) + `"`} {
			if !strings.Contains(missing, want) {
				t.Errorf("%s: not-found state misses %q: %s", locale, want, missing)
			}
		}
		for _, want := range []string{words[2], words[3], words[4]} {
			if !strings.Contains(failed, want) {
				t.Errorf("%s: failed state misses %q: %s", locale, want, failed)
			}
		}
		for _, page := range []string{missing, failed} {
			if strings.Contains(strings.ToLower(page), "context canceled") || strings.Contains(page, "⟦") {
				t.Errorf("%s: a route state prints internal text: %s", locale, page)
			}
		}
		if strings.Contains(missing, "<button") {
			t.Errorf("%s: the not-found state offers a retry", locale)
		}
		withDetail, _ := ui.RenderToString(RouteLoadFailed(ctx, nil, "Reviewed denial copy."))
		if !strings.Contains(withDetail, "Reviewed denial copy.") || strings.Contains(withDetail, "<button") {
			t.Errorf("%s: a reviewed detail did not replace the generic message, or a retry showed with none: %s", locale, withDetail)
		}
	}
}
