package journey

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestTodo_UXBLIND_002_Browser(t *testing.T) {
	for _, locale := range productui.SupportedProductLocales() {
		t.Run(locale, func(t *testing.T) {
			copy := productui.ResolveProductLocale(locale)
			p := Page{Locale: locale, Notice: &Notice{
				Tone: "danger", Title: copy.Text("journey.error_domain_unavailable_title"),
				Detail: copy.Text("journey.error_domain_unavailable_detail"),
			}}
			p.Detail = &DetailView{
				Journey:  JourneyCard{WorkerName: "Avery", Stage: "PROPOSED", StageLabel: "Ready to start approval"},
				Timeline: []TimelineEvent{{At: "28 Sep 2026, 5:53 PM", Actor: "Avery", Title: copy.Text("journey.timeline_start_failed"), Detail: copy.Text("journey.timeline_start_failed_domain"), Tone: "danger"}},
			}
			markup, err := ui.RenderToString(Build(p))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{
				copy.Text("journey.error_domain_unavailable_title"),
				copy.Text("journey.error_domain_unavailable_detail"),
				copy.Text("journey.timeline_start_failed"),
				copy.Text("journey.timeline_start_failed_domain"),
				`data-tone="danger"`,
			} {
				if !strings.Contains(markup, want) {
					t.Errorf("failure page missing %q: %s", want, markup)
				}
			}
			if strings.Contains(markup, "<details class=\"jn-confirm\" open") {
				t.Fatalf("failure page left confirmation open: %s", markup)
			}
		})
	}
}

func TestTodo_UXBLIND_002_Golden(t *testing.T) {
	copy := productui.ResolveProductLocale("en-US")
	view := DetailView{
		Journey:  JourneyCard{WorkerName: "Avery", Stage: "PROPOSED", StageLabel: "Ready to start approval"},
		Timeline: []TimelineEvent{{At: "28 Sep 2026, 5:53 PM", Actor: "Avery", Title: copy.Text("journey.timeline_start_failed"), Detail: copy.Text("journey.timeline_start_failed_domain"), Tone: "danger"}},
	}
	markup, err := ui.RenderToString(Build(Page{Locale: "en-US", Notice: &Notice{
		Tone: "danger", Title: copy.Text("journey.error_domain_unavailable_title"), Detail: copy.Text("journey.error_domain_unavailable_detail"),
	}, Detail: &view}))
	if err != nil {
		t.Fatal(err)
	}
	golden := []string{
		`Approval service unavailable`,
		`The approval service could not be reached. No change was made; try again later.`,
		`Approval start failed`,
		`The approval service was unavailable.`,
		`Avery`,
		`data-tone="danger"`,
	}
	for _, want := range golden {
		if !strings.Contains(markup, want) {
			t.Errorf("golden failure page missing %q: %s", want, markup)
		}
	}
}
