package journey

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestTodo_UXBLIND_076_Browser(t *testing.T) {
	for _, locale := range productui.SupportedProductLocales() {
		t.Run(locale, func(t *testing.T) {
			copy := productui.ResolveProductLocale(locale)
			markup, err := ui.RenderToString(detailView(Page{Locale: locale}, DetailView{
				Journey: JourneyCard{WorkerName: "Avery"},
				Timeline: []TimelineEvent{{
					At: "28 Sep 2026, 5:53 PM", Actor: "Avery",
					Title:  copy.Text("journey.timeline_start_failed"),
					Detail: copy.Text("journey.timeline_start_failed_domain"), Tone: "danger",
				}},
			}))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{copy.Text("journey.history_heading"), copy.Text("journey.timeline_start_failed"), copy.Text("journey.timeline_start_failed_domain"), "Avery", `data-tone="danger"`} {
				if !strings.Contains(markup, want) {
					t.Errorf("history markup missing %q: %s", want, markup)
				}
			}
		})
	}
}
