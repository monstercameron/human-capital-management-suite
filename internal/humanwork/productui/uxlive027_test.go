package productui

import (
	"strconv"
	"strings"
	"testing"
)

// TestTodo_UXLIVE_027_Browser renders the full shell for Journeys, Insights
// and Home over one view -- one session, one server summary -- and checks the
// three pages agree: the tracker lists every journey the summary counts,
// Insights reports that total, and neither summary page prints empty copy.
// It also pins the one narrower scope the pages may use: when per-record
// verdicts hide a journey, the totals follow the admitted population rather
// than a server count that includes a record the viewer cannot open.
func TestTodo_UXLIVE_027_Browser(t *testing.T) {
	base := uxlive030View()
	total := base.JourneyPopulation.Total

	journeys := base
	journeys.Page = PageJourneys
	tracker := uxlive030Render(t, Build(journeys))
	for _, item := range base.Work {
		if !strings.Contains(tracker, item.Person) {
			t.Fatalf("the Journeys tracker omits %s, which the summary counts", item.Person)
		}
	}

	insights := base
	insights.Page = PageInsights
	insightsMarkup := uxlive030Render(t, Build(insights))
	if strings.Contains(insightsMarkup, base.Locale.Text("insights.no_data_title")) {
		t.Fatalf("Insights printed its empty copy over %d visible journeys", total)
	}
	visible := base.Locale.Text("insights.visible_label") + `</span><strong>` + strconv.Itoa(total) + `</strong>`
	if !strings.Contains(insightsMarkup, visible) {
		t.Fatalf("Insights does not report the %d journeys Journeys lists:\n%s", total, insightsMarkup)
	}

	home := base
	homeMarkup := uxlive030Render(t, Build(home))
	if strings.Contains(homeMarkup, base.Locale.Text("home.empty_title")) {
		t.Fatalf("Home claimed no work in progress over %d visible journeys", total)
	}

	// A verdict map that hides one journey narrows every total to what the
	// viewer can open, and says so by using the admitted population.
	scoped := uxlive030View()
	scoped.RecordVerdicts = map[string]AuthorizedRecord{}
	for _, item := range scoped.Work {
		scoped.RecordVerdicts[item.ID] = AuthorizedRecord{ID: item.ID, Disclosable: item.ID != "repair-run"}
	}
	totals, fromServer := journeyTotals(scoped)
	if fromServer || totals.Total != total-1 || totals.Exceptions != base.JourneyPopulation.Exceptions-1 {
		t.Fatalf("verdict-scoped totals = %+v (server %t), want the %d admitted journeys", totals, fromServer, total-1)
	}
	scoped.Page = PageInsights
	if !strings.Contains(uxlive030Render(t, Build(scoped)), base.Locale.Text("insights.visible_label")+`</span><strong>`+strconv.Itoa(total-1)+`</strong>`) {
		t.Fatal("Insights counted a journey the viewer may not open")
	}
}
