package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UIPOLISH_007_HistoryLocalizesColumnsAndRecordedOutcome(t *testing.T) {
	for _, tc := range []struct {
		locale, outcome, effective, closed string
		columns                            []string
	}{
		{"en-US", "Recorded", "2026-12-01", "1 Dec 2026 · 12:00 UTC", []string{"Employee", "Change", "Closed", "Outcome"}},
		{"de-DE", "Erfasst", "01.12.2026", "01.12.2026 · 07:00 EST", []string{"Mitarbeitende", "Änderung", "Geschlossen", "Ergebnis"}},
		{"ar", "مسجل", "١ ديسمبر ٢٠٢٦", "١ ديسمبر ٢٠٢٦ · \u2066٠٧:٠٠ EST\u2069", []string{"الموظف", "التغيير", "أُغلق", "النتيجة"}},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			view := ApplyLocale(testView(PageHistory), ResolveProductLocale(tc.locale))
			view.Locale.TimeZone = "America/New_York"
			view.Work = []WorkItem{{
				ID: "recorded", Title: "Promotion journey", TitleKey: "journey.detail_title",
				Status: "Recorded", StatusKey: "journey.stage_recorded", Terminal: true,
				Person: "Avery Patel", PersonRef: "worker-avery", EffectiveDate: "2026-12-01", CompletedAt: "1 Dec 2026 · 12:00 UTC",
			}}
			props := workflowHistoryProps(view, "", "History", "", true)
			if len(props.Items) != 1 || props.Items[0].Outcome != tc.outcome {
				t.Fatalf("%s recorded outcome = %+v, want %q", tc.locale, props.Items, tc.outcome)
			}
			if got := props.Items[0].EffectiveDate; got != tc.effective {
				t.Errorf("%s effective date = %q, want %q", tc.locale, got, tc.effective)
			}
			if got := props.Items[0].CompletedAt; got != tc.closed {
				t.Errorf("%s closed date = %q, want %q", tc.locale, got, tc.closed)
			}
			if len(props.Columns) != len(tc.columns) {
				t.Fatalf("%s columns = %+v", tc.locale, props.Columns)
			}
			for index, want := range tc.columns {
				if got := props.Columns[index].Label; got != want {
					t.Errorf("%s column %d = %q, want %q", tc.locale, index, got, want)
				}
			}
			view.HistoryOutcome = "completed"
			filtered := workflowHistoryProps(view, "", "History", "", true)
			if filtered.FilteredCount != 1 || len(filtered.Items) != 1 {
				t.Fatalf("%s Completed filter excluded Recorded: %+v", tc.locale, filtered)
			}
			for _, visible := range []string{tc.outcome, tc.effective} {
				view.HistoryQuery = visible
				searched := workflowHistoryProps(view, "", "History", "", true)
				if searched.FilteredCount != 1 {
					t.Errorf("%s search excluded visible row text %q", tc.locale, visible)
				}
			}
		})
	}
}

func TestTodo_UIPOLISH_007_ArabicHistoryHasNoEnglishPageOrFilterFallback(t *testing.T) {
	view := ApplyLocale(testView(PageHistory), ResolveProductLocale("ar"))
	markup, err := ui.RenderToString(historyPage(view))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"سجل مسارات العمل", "كل الموظفين", "ترقية", "فتح السجل"} {
		if !strings.Contains(markup, want) {
			t.Errorf("Arabic History missing %q", want)
		}
	}
	for _, leaked := range []string{"Global workflow history", "All employees", "Promotion", "Open record"} {
		if strings.Contains(markup, leaked) {
			t.Errorf("Arabic History retained English UI copy %q", leaked)
		}
	}
	if got := view.Locale.Text("page.history.subtitle"); got != "راجع مسارات العمل المكتملة والمرفوضة والفاشلة." {
		t.Errorf("Arabic History subtitle = %q", got)
	}
	if got := historyCountLabel(view.Locale, 2, 2); got != "سجلان" {
		t.Errorf("Arabic exact-two record count = %q", got)
	}
	if got := historyCountLabel(view.Locale, 0, 2); got != "٠ من أصل ٢" {
		t.Errorf("Arabic filtered record count = %q", got)
	}
}
