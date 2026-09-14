package productui

import (
	"strings"
	"testing"
)

func TestTodo_UXSCAN_007(t *testing.T) {
	view := testView(PageInsights)
	view.Work = nil
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`class="surface empty-state insights-empty"`,
		"No journeys to summarize in your view",
		"Your current access scope contains no visible promotion journeys",
		"does not establish an organization-wide count",
		`href="/workspace/app/people?eligible=1"`,
		"Update time not available",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("empty Insights missing %q", want)
		}
	}
	if strings.Contains(doc, `class="metrics"`) || strings.Contains(doc, "Not reported") {
		t.Fatal("empty Insights still shows repeated unknown metrics")
	}
}

func TestTodo_UXSCAN_007_Integration(t *testing.T) {
	view := testView(PageInsights)
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`Visible workflows</span><strong>2</strong>`,
		`In progress</span><strong>1</strong>`,
		`Completed or closed</span><strong>1</strong>`,
		"Promotion journeys you can access",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("populated Insights lost evidence %q", want)
		}
	}
}

func TestTodo_UXSCAN_007_Accessibility(t *testing.T) {
	view := testView(PageInsights)
	view.Work = nil
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `<h2>No journeys to summarize in your view</h2>`) ||
		!strings.Contains(doc, `>Find an employee to promote</a>`) {
		t.Fatal("empty Insights lacks semantic heading or named action")
	}
	for _, locale := range []string{"de-DE", "ar"} {
		localized := ApplyLocale(view, ResolveProductLocale(locale))
		doc, err := Render(localized)
		if err != nil || strings.Contains(doc, "⟦") || !strings.Contains(doc, localized.Locale.Text("insights.no_data_title")) {
			t.Errorf("%s empty Insights not localized: %v", locale, err)
		}
	}
}

func TestTodo_UXSCAN_007_Security(t *testing.T) {
	view := testView(PageInsights)
	view.Work = nil
	view.LauncherActions = nil
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "Find an employee to promote") || strings.Contains(doc, `eligible=1`) {
		t.Fatal("promotion start disclosed without server semantic authority")
	}
	view.Work = []WorkItem{{ID: "hidden", Person: "Private person"}}
	view.RecordVerdicts = map[string]AuthorizedRecord{"hidden": {ID: "hidden", Disclosable: false}}
	doc, err = Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "Private person") || strings.Contains(doc, "1 hidden") {
		t.Fatal("empty Insights disclosed suppressed records")
	}
}

func TestTodo_UXSCAN_007_Regression(t *testing.T) {
	view := testView(PageInsights)
	view.Work = nil
	view.LoadError = "service unavailable"
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "No journeys to summarize in your view") {
		t.Fatal("failed read misreported as known empty")
	}
}
