package productui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
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
		`Promotion requests</span><strong>2</strong>`,
		`In progress</span><strong>1</strong>`,
		`Completed or closed</span><strong>1</strong>`,
		"Period", "Last updated", "Includes", "Current view; no historical period selected",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("insights summary missing %q", want)
		}
	}
	// UXBLIND-106: the workforce snapshot bar rows carry their own inline track; the ban applies to the insights grid itself.
	if !strings.Contains(Stylesheet(), `.insights-grid{display:grid;gap:18px;grid-template-columns:minmax(0,1fr);}`) || strings.Contains(doc, `class="insights-grid" style=`) {
		t.Fatal("insights must use a stylesheet-owned full-width track, not an inline layout override")
	}
	unsupported, err := insightsContainsUnsupportedAnalyticsText(doc)
	if err != nil {
		t.Fatal(err)
	}
	if unsupported {
		t.Fatal("workflow summary fabricated unsupported analytics")
	}
}

func insightsContainsUnsupportedAnalyticsText(markup string) (bool, error) {
	root, err := xhtml.Parse(strings.NewReader(markup))
	if err != nil {
		return false, err
	}
	var insights *xhtml.Node
	var findInsights func(*xhtml.Node)
	findInsights = func(node *xhtml.Node) {
		if insights != nil {
			return
		}
		if node.Type == xhtml.ElementNode {
			for _, class := range strings.Fields(attr(node, "class")) {
				if class == "insights-grid" {
					insights = node
					return
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			findInsights(child)
		}
	}
	findInsights(root)
	if insights == nil {
		return false, fmt.Errorf("Insights section was not found")
	}
	var visibleText strings.Builder
	var collectText func(*xhtml.Node)
	collectText = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode && (node.Data == "style" || node.Data == "script") {
			return
		}
		if node.Type == xhtml.TextNode {
			visibleText.WriteString(node.Data)
			visibleText.WriteByte(' ')
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			collectText(child)
		}
	}
	collectText(insights)
	text := strings.ToLower(strings.Join(strings.Fields(visibleText.String()), " "))
	for _, forbidden := range []string{"attrition", "trend", "workforce analytics"} {
		if strings.Contains(text, forbidden) {
			return true, nil
		}
	}
	return false, nil
}

func TestTodo_UXAUDIT_018_Regression_ForbiddenTermsAreScopedToVisibleInsightsText(t *testing.T) {
	markup := `<style>.chatrender-trend{display:grid}</style><main><section class="insights-grid"><p>Current workflow summary.</p></section></main>`
	unsupported, err := insightsContainsUnsupportedAnalyticsText(markup)
	if err != nil || unsupported {
		t.Fatalf("CSS selector was treated as Insights copy: unsupported=%v err=%v", unsupported, err)
	}
	markup = `<style>.chatrender-personal{display:grid}</style><main><section class="insights-grid"><p>Historical trend</p></section></main>`
	unsupported, err = insightsContainsUnsupportedAnalyticsText(markup)
	if err != nil || !unsupported {
		t.Fatalf("visible unsupported Insights copy was not rejected: unsupported=%v err=%v", unsupported, err)
	}
	markup = `<main><section class="insights-grid"><p>Historical TREND</p></section></main>`
	unsupported, err = insightsContainsUnsupportedAnalyticsText(markup)
	if err != nil || !unsupported {
		t.Fatalf("case variation of visible unsupported copy was not rejected: unsupported=%v err=%v", unsupported, err)
	}
	if _, err = insightsContainsUnsupportedAnalyticsText(`<main><p>No Insights section</p></main>`); err == nil {
		t.Fatal("missing Insights section was treated as a passing scan")
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

func TestInsightsEvidenceLayoutKeepsFactsInsideThePanel(t *testing.T) {
	doc, err := Render(testView(PageInsights))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`class="surface panel insights-evidence"`, `class="insights-evidence-body"`, `class="muted insights-evidence-note"`, `<dl class="facts">`} {
		if !strings.Contains(doc, want) {
			t.Fatalf("insights evidence missing %q", want)
		}
	}
	css := Stylesheet()
	for _, want := range []string{`.insights-evidence-body{`, `.insights-evidence .facts>div{`, `.insights-evidence .facts dd{`, `.insights-evidence-note{`} {
		if !strings.Contains(css, want) {
			t.Fatalf("insights evidence stylesheet missing %q", want)
		}
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
	if strings.Contains(doc, "Private worker") || !strings.Contains(doc, `Promotion requests</span><strong>1</strong>`) || !strings.Contains(doc, `In progress</span><strong>1</strong>`) {
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
	for _, want := range []string{"No journeys to summarize in your view", "Your current access scope contains no visible promotion journeys", "does not establish an organization-wide count"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("empty Insights state missing %q", want)
		}
	}
	if strings.Contains(doc, "Not reported") || strings.Contains(doc, `class="metrics"`) {
		t.Fatal("empty Insights state regressed to repeated unreported metrics")
	}

	view.LoadError = "service unavailable"
	doc, err = Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "No journeys to summarize in your view") {
		t.Fatal("failed read was presented as a known-empty view")
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
