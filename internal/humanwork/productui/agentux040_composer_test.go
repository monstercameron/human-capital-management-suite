package productui

import (
	"regexp"
	"strings"
	"testing"
)

var agentUX040Rule = regexp.MustCompile(`([^{}]+)\{([^{}]*)\}`)

// agentUX040HidesEmptyControl reports whether a selector that hides :empty
// elements can match an empty question field: a textarea, input or select has
// no children, so :empty matches it unless the selector rules it out.
func agentUX040HidesEmptyControl(selector string) bool {
	if !strings.Contains(selector, ":empty") {
		return false
	}
	// A selector that names a class or element other than a form control on the
	// :empty compound cannot match the composer's textarea.
	compound := selector[strings.LastIndexAny(selector, " >+~")+1:]
	if strings.HasPrefix(compound, ":empty") || strings.HasPrefix(compound, "*:empty") {
		for _, control := range []string{":not(textarea)", ":not(input)", ":not(select)"} {
			if !strings.Contains(compound, control) {
				return true
			}
		}
		return false
	}
	for _, control := range []string{"textarea", "input", "select"} {
		if strings.HasPrefix(compound, control) {
			return true
		}
	}
	return false
}

// The Agents page offered an Ask button and no visible place to type: a rule
// hiding every empty child of the composer also hid the empty question field.
func TestTodo_AGENTUX_040(t *testing.T) {
	// No rule in the stylesheet the page is served with hides an empty form
	// control inside the composer.
	sheet := Stylesheet()
	checked := 0
	for _, rule := range agentUX040Rule.FindAllStringSubmatch(sheet, -1) {
		if !strings.Contains(strings.ReplaceAll(rule[2], " ", ""), "display:none") {
			continue
		}
		for _, selector := range strings.Split(rule[1], ",") {
			selector = strings.TrimSpace(selector)
			if !strings.Contains(selector, ":empty") {
				continue
			}
			checked++
			if agentUX040HidesEmptyControl(selector) && (strings.Contains(selector, "agents-composer") || strings.Contains(selector, "agent-page-frame") || !strings.Contains(selector, ".")) {
				t.Errorf("the rule %q hides an empty form control", selector)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no :empty rule was examined; the stylesheet scan is not looking at the served sheet")
	}
	if !strings.Contains(sheet, `.agents-composer > :empty:not(textarea):not(input):not(select){display:none;}`) {
		t.Fatal("the composer's empty-line rule no longer excludes form controls")
	}
	// The scan itself tells a hiding rule from a safe one.
	for selector, hides := range map[string]bool{
		".agents-composer > :empty":                                      true,
		".agents-composer > :empty:not(textarea):not(input):not(select)": false,
		".agents-composer textarea:empty":                                true,
		".agents-tasks-announcement:empty":                               false,
		".agents-composer > *:empty":                                     true,
		".agents-composer p:empty":                                       false,
	} {
		if agentUX040HidesEmptyControl(selector) != hides {
			t.Errorf("selector %q judged %t, want %t", selector, !hides, hides)
		}
	}

	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		// The field is there to type in, labelled, enabled, and not hidden, for
		// every reader in every language.
		composer := agentUXR7Render(t, tagAgentComposer(locale, AgentSnapshot{StartAvailable: true}))
		field := regexp.MustCompile(`<textarea[^>]*>`).FindString(composer)
		if field == "" || !strings.Contains(field, `id="agents-composer-input"`) || strings.Contains(field, "hidden") || strings.Contains(field, "disabled") || strings.Contains(field, "readonly") {
			t.Fatalf("%s: the question field is missing, hidden or not editable: %q", language, field)
		}
		if !strings.Contains(composer, `<label for="agents-composer-input">`+locale.Text("agents.composer_label")+`</label>`) {
			t.Fatalf("%s: the question field has no visible label", language)
		}
		if !strings.Contains(field, `placeholder="`) {
			t.Fatalf("%s: the question field gives no hint of what to type", language)
		}

		// The default tab is the first of Active, Completed, Failed with rows.
		for _, tc := range []struct {
			tasks []AgentTask
			want  string
		}{
			{[]AgentTask{{ID: "f", Title: "Failed one", State: AgentTaskFailed}}, "failed"},
			{[]AgentTask{{ID: "f", Title: "Failed one", State: AgentTaskFailed}, {ID: "c", Title: "Done one", State: AgentTaskCompleted, AnswerText: "Done."}}, "completed"},
			{[]AgentTask{{ID: "f", Title: "Failed one", State: AgentTaskFailed}, {ID: "c", Title: "Done one", State: AgentTaskCompleted, AnswerText: "Done."}, {ID: "a", Title: "Running one", State: AgentTaskRunning}}, "active"},
			{nil, "active"},
		} {
			page := renderAgentUXPage(t, language, AgentsAvailabilityProjection{Enabled: true, Snapshot: AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true, Tasks: tc.tasks}})
			if !strings.Contains(page, `data-selected-task-filter="`+tc.want+`"`) {
				t.Fatalf("%s: with %d tasks the default tab is not %q", language, len(tc.tasks), tc.want)
			}
			// Nothing is selected, so nothing is expanded and no detail is drawn.
			if strings.Contains(page, `aria-expanded="true"`) || strings.Contains(page, `class="agents-task-detail-pane"`) || !strings.Contains(page, `data-selected-task-id=""`) {
				t.Fatalf("%s: a page opened without a task selected one", language)
			}
		}
	}
}

// The field keeps its room at every width: the composer's layout rules are in
// the served stylesheet and none of the narrow-width rules removes the field.
func TestTodo_AGENTUX_040_Browser(t *testing.T) {
	sheet := Stylesheet()
	for _, want := range []string{
		`.agents-composer textarea{`, `min-height:96px`, `resize:vertical`,
		`.agents-composer,.agents-tasks,.agents-task-view{box-sizing:border-box;width:100%`,
	} {
		if !strings.Contains(sheet, want) {
			t.Fatalf("the served stylesheet is missing %q", want)
		}
	}
	for _, rule := range agentUX040Rule.FindAllStringSubmatch(sheet, -1) {
		if !strings.Contains(rule[1], ".agents-composer textarea") && !strings.Contains(rule[1], "#agents-composer-input") {
			continue
		}
		body := strings.ReplaceAll(rule[2], " ", "")
		for _, hiding := range []string{"display:none", "visibility:hidden", "height:0", "block-size:0", "opacity:0"} {
			if strings.Contains(body, hiding) {
				t.Fatalf("the rule %q{%s} hides the question field", strings.TrimSpace(rule[1]), rule[2])
			}
		}
	}
}
