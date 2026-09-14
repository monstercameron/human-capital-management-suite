package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestHistoryEffectiveYearRejectsUnparseablePrefixes(t *testing.T) {
	for _, input := range []string{"", "20", "202x-01-01", "unknown-2026"} {
		if got := historyEffectiveYear(input); got != "" {
			t.Fatalf("historyEffectiveYear(%q) = %q, want empty", input, got)
		}
	}
	if got := historyEffectiveYear("2026-08-01"); got != "2026" {
		t.Fatalf("historyEffectiveYear(valid) = %q, want 2026", got)
	}
}

func TestHistorySortValueOrdersCanonicalTimestamps(t *testing.T) {
	items := []WorkItem{
		{ID: "old", CompletedAt: "2026-01-02T12:00:00Z"},
		{ID: "new", CompletedAt: "3 Jan 2026 · 12:00 UTC"},
	}
	sortHistory(items, historySortClosed, "desc")
	if items[0].ID != "new" {
		t.Fatalf("canonical timestamp sort = %q first, want new", items[0].ID)
	}
}

func TestWorkflowHistoryItemKeepsEvidenceInspectableWithoutDeadAction(t *testing.T) {
	markup, err := ui.RenderToString(ui.CreateElement(WorkflowHistoryItem, WorkflowHistoryItemProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		Person:    "Avery Patel", Outcome: "Completed", Tone: "success",
		Provenance: ProvenanceProjection{Bound: true},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"class=\"history-evidence\"", ">Provenance<", "history-unavailable"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("history item missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, `href=""`) {
		t.Fatalf("history item emitted a dead action link: %s", markup)
	}
}
