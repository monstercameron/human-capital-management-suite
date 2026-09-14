package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestTodo_UXAUDIT_018 proves that the production surface is a bounded
// workflow summary: every measure has an explanation and the evidence panel
// states the snapshot period, freshness and source instead of implying a
// broader analytics report.
func TestTodo_UXAUDIT_018(t *testing.T) {
	doc, err := Render(testView(PageInsights))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`class="insights-grid"`,
		`class="insights-summary"`,
		`Visible workflows</span><strong>2</strong>`,
		`In progress</span><strong>1</strong>`,
		`Completed or closed</span><strong>1</strong>`,
		"Period", "Last updated", "Includes", "Current view; no historical period selected",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("insights summary missing %q", want)
		}
	}
	if !strings.Contains(Stylesheet(), `.insights-grid{display:grid;gap:18px;grid-template-columns:minmax(0,1fr);}`) || strings.Contains(doc, `style="grid-template-columns`) {
		t.Fatal("insights must use a stylesheet-owned full-width track, not an inline layout override")
	}
	if strings.Contains(doc, "attrition") || strings.Contains(doc, "trend") || strings.Contains(doc, "workforce analytics") {
		t.Fatal("workflow summary fabricated unsupported analytics")
	}
}

func TestTodo_UXAUDIT_018_Accessibility(t *testing.T) {
	props := InsightsPageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		Metrics:   []MetricProps{{Label: "Visible workflows", Value: "1", Note: "Authorized"}},
		Attention: AttentionPanelProps{Title: "Needs attention", CountLabel: "Needs attention", CountValue: "Not reported", Description: "A bounded summary."},
		Evidence:  InsightsEvidenceProps{TimeRange: "Current view", Freshness: "Unavailable", Lineage: "Authorized workflows"},
	}
	doc, err := ui.RenderToString(ui.CreateElement(InsightsPage, props))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "<section") || !strings.Contains(doc, "<h2>") || !strings.Contains(doc, "<dl") {
		t.Fatalf("insights composition lost semantic landmarks: %s", doc)
	}
}

func TestTodo_UXAUDIT_018_Security(t *testing.T) {
	view := testView(PageInsights)
	view.Work = []WorkItem{{ID: "visible", Status: "Blocked"}, {ID: "denied", Status: "Awaiting approval", Person: "Private worker"}}
	view.RecordVerdicts = map[string]AuthorizedRecord{
		"visible": {ID: "visible", Disclosable: true},
		"denied":  {ID: "denied", Disclosable: false},
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	// The shell may retain an unrelated navigation badge from the fixture's
	// original work collection. Assert the Insights measures themselves rather
	// than treating every numeric token in the full document as page data.
	if strings.Contains(doc, "Private worker") || !strings.Contains(doc, `Visible workflows</span><strong>1</strong>`) || !strings.Contains(doc, `In progress</span><strong>1</strong>`) {
		t.Fatal("denied workflow influenced Insights")
	}
}

func TestTodo_UXAUDIT_018_Regression_UnassignedVisibleWorkIsNotAttention(t *testing.T) {
	view := testView(PageInsights)
	view.Viewer = ViewerProfile{PersonID: "rafael", Name: "Rafael Torres"}
	view.Work = []WorkItem{
		{ID: "visible-review", Status: "Awaiting approval", PersonRef: "another-worker", NextStep: "approval_decision", ViewerResponsibility: "OBSERVING"},
		{ID: "assigned-review", Status: "Awaiting approval", AssigneeRef: "rafael", NextStep: "approval_decision", ViewerResponsibility: "ACTION_REQUIRED", ViewerRelationships: []string{"ASSIGNEE"}},
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `<dt>Needs attention</dt><dd>1</dd>`) {
		t.Fatalf("Insights attention count did not follow My Work assignment scope: %s", doc)
	}
}

func TestTodo_UXAUDIT_018_Regression_EmptyAttentionExplainsNextStep(t *testing.T) {
	view := testView(PageInsights)
	view.Viewer = ViewerProfile{PersonID: "rafael", Name: "Rafael Torres"}
	view.Work = []WorkItem{{ID: "visible-review", Status: "Awaiting approval", PersonRef: "another-worker", NextStep: "approval_decision", ViewerResponsibility: "OBSERVING"}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "New assignments will appear here") {
		t.Fatal("empty attention state omitted the next-step explanation")
	}
	if !strings.Contains(doc, "This summary covers promotion journeys you can view") {
		t.Fatal("scope disclaimer was not retained in evidence context")
	}
}

func TestTodo_UXAUDIT_018_Regression(t *testing.T) {
	view := testView(PageInsights)
	view.Work = nil
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Not enough information yet", "Not enough visible requests to report a count", "a missing count is not zero"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("insufficient-data state missing %q", want)
		}
	}
	if strings.Contains(doc, ">0<") {
		t.Fatal("insufficient-data state regressed to a zero facade")
	}

	view.LoadError = "service unavailable"
	doc, err = Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "Not enough information yet") {
		t.Fatal("failed read was presented as insufficient data")
	}
}

func TestTodo_UXAUDIT_018_I18N(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := ApplyLocale(testView(PageInsights), ResolveProductLocale(locale))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("unresolved Insights copy in %s", locale)
		}
		if !strings.Contains(doc, view.Locale.Text("insights.context_title")) {
			t.Fatalf("evidence context is not localized in %s", locale)
		}
	}
}
