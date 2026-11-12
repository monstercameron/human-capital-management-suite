package productui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// optionTagPattern matches each rendered <option ...> start tag.
var optionTagPattern = regexp.MustCompile(`<option\b[^>]*>`)

// assertPlaceholderOptionsCarryValue fails when any rendered <option> lacks a
// value attribute, or when the placeholder option has no explicit empty value.
// Without the attribute the browser submits the visible label as the field value.
func assertPlaceholderOptionsCarryValue(t *testing.T, name, markup string) {
	t.Helper()
	tags := optionTagPattern.FindAllString(markup, -1)
	if len(tags) == 0 {
		t.Fatalf("%s: rendered no <option> elements: %s", name, markup)
	}
	emptyValues := 0
	for _, tag := range tags {
		if !strings.Contains(tag, " value=") {
			t.Fatalf("%s: option rendered without a value attribute (browser submits its label): %s", name, tag)
		}
		if strings.Contains(tag, ` value=""`) {
			emptyValues++
		}
	}
	if emptyValues == 0 {
		t.Fatalf("%s: no placeholder option carries an explicit empty value: %s", name, markup)
	}
}

// TestIssue3634_PlaceholderOptionsSubmitEmptyValue renders every productui
// select whose placeholder option was built with html.Props{Value: ""}, which
// the GoWebComponents html package drops. Each rendered option must keep value="".
func TestIssue3634_PlaceholderOptionsSubmitEmptyValue(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	cases := []struct {
		name   string
		render func() ui.Node
	}{
		{"agent announcement editor", func() ui.Node { return renderAgentAnnouncementEditor(locale, nil) }},
		{"agent run history filters", func() ui.Node { return RenderAgentRunHistory(locale, nil, AgentRunHistoryFilter{}) }},
		{"persona admin agent select", func() ui.Node { return personaAdminAgentSelect(locale, "", false, false, nil) }},
		{"persona admin simple select", func() ui.Node {
			return personaAdminSimpleSelect(locale, "suffix", "persona_admin_field", "", false, nil)
		}},
		{"benefit enrollment form", func() ui.Node { return benefitEnrollForm(View{}, BenefitsProjection{}, nil, nil) }},
		{"docs compare dialog", func() ui.Node { return docsCompareDialog(docsCompareDialogProps{Locale: "en-US"}) }},
		{"workflow history filter", func() ui.Node {
			return WorkflowHistoryFilter(WorkflowHistoryFilterProps{I18nProps: I18nProps{Locale: locale}})
		}},
		{"pay discrepancy form", func() ui.Node { return payDiscrepancyForm(View{}, PayDiscrepancyProjection{}) }},
		{"HR service request page", func() ui.Node { return HRServiceRequestPage(HRServiceRequestPageProps{}) }},
		{"persona admin editor", func() ui.Node {
			snapshot := PersonaAdminSnapshot{StarterCatalogAvailable: true, Starters: []PersonaAdminStarter{{
				ID: "starter-1", Version: 1, Name: "Starter", Handle: "starter", Purpose: "Answer questions",
				ManifestID: "manifest-1", Instructions: "Help", ChannelClasses: []string{"chat"}, SkillGrantIDs: []string{"grant-1"},
			}}}
			return PersonaAdminEditor(locale, nil, snapshot)
		}},
		{"position option selector", func() ui.Node { return positionOptionSelector(View{}) }},
		{"workflow run history filters", func() ui.Node {
			return workflowHistoryFilters(View{}, WorkflowRunHistoryFilter{})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			markup, err := ui.RenderToString(tc.render())
			if err != nil {
				t.Fatal(err)
			}
			assertPlaceholderOptionsCarryValue(t, tc.name, markup)
		})
	}
}

// TestIssue3634_NoValuelessPlaceholderOptionSource guards against a new
// html.Option(html.Props{Value: ""...}) call returning. The html package drops
// the empty Value, so such a placeholder submits its label; use
// html.Props{Raw: map[string]any{"value": ""}} instead.
func TestIssue3634_NoValuelessPlaceholderOptionSource(t *testing.T) {
	const forbidden = `html.Option(html.Props{Value: ""`
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	scanned := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		scanned++
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("%s uses %s; the html package drops the empty Value so the option submits its label. Use html.Props{Raw: map[string]any{\"value\": \"\"}}", file, forbidden)
		}
	}
	if scanned == 0 {
		t.Fatal("guard scanned no non-test source files")
	}
}
