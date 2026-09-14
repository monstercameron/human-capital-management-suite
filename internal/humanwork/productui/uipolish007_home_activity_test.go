package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UIPOLISH_007_HomeActivityUsesSemanticLocaleKeys(t *testing.T) {
	for _, tc := range []struct {
		locale, title, status string
	}{
		{"en-US", "Promotion journey", "Recorded"},
		{"de-DE", "Beförderungsantrag", "Erfasst"},
		{"ar", "طلب الترقية", "مسجل"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			view := ApplyLocale(testView(PageHome), ResolveProductLocale(tc.locale))
			view.Work = []WorkItem{{
				ID: "recorded", Title: "Promotion journey", TitleKey: "journey.detail_title",
				Status: "Recorded", StatusKey: "journey.stage_recorded", Terminal: true,
			}}
			markup, err := ui.RenderToString(homePage(view))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(markup, ">"+tc.title+"<") || !strings.Contains(markup, ">"+tc.status+"<") {
				t.Fatalf("%s recent activity ignored semantic keys: %s", tc.locale, markup)
			}
			if tc.locale != "en-US" && strings.Contains(markup, ">Promotion journey<") {
				t.Fatalf("%s recent activity leaked English adapter title: %s", tc.locale, markup)
			}
		})
	}
	if got := localizedWorkTitle(ResolveProductLocale("de-DE"), WorkItem{Title: "Other governed workflow"}); got != "Other governed workflow" {
		t.Fatalf("unkeyed workflow title changed: %q", got)
	}
}
