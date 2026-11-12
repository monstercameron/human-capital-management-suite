package chatui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// optionTagPattern matches each rendered <option ...> start tag.
var optionTagPattern = regexp.MustCompile(`<option\b[^>]*>`)

// assertPlaceholderOptionsCarryValue fails when any rendered <option> lacks a
// value attribute, or when no placeholder option has an explicit empty value.
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

// TestIssue3634_PlaceholderOptionsSubmitEmptyValue renders every chat surface
// whose placeholder option was built with html.Props{Value: ""}, which the
// GoWebComponents html package drops. Each rendered option must keep value="".
// The chat search "All kinds" and "Any language" controls (#3403) are covered
// by the same assertion.
func TestIssue3634_PlaceholderOptionsSubmitEmptyValue(t *testing.T) {
	view := chatgateViewFixture()
	definition := view.Gate.Versions[0]
	cases := []struct {
		name   string
		render func() ui.Node
	}{
		{"gate view", func() ui.Node { return RenderGate(view) }},
		{"gate builder", func() ui.Node { return renderGateBuilder(view) }},
		{"gate rule editor", func() ui.Node { return gateRuleEditor(view, definition) }},
		{"gate segments", func() ui.Node {
			return renderGateSegments(chatgateSegmentFixture())
		}},
		{"gate override", func() ui.Node { return html.Div(html.Props{}, renderGateOverride(view)...) }},
		{"gate fields", func() ui.Node {
			nodes := []ui.Node{}
			for _, field := range definition.Fields {
				nodes = append(nodes, renderGateField(view, field))
			}
			return html.Div(html.Props{}, nodes...)
		}},
		{"gate composer", func() ui.Node {
			m := Model{Locale: "en-US", Conversations: []Conversation{{ID: "room", Kind: PublicChannel, Name: "Room", Joined: true}},
				SelectedID: "room", GateAudience: map[string]GateAudienceOptions{"room": {Segments: []GateAudienceChoice{{Name: "Denver crew", People: 2}}}}}
			return chatgate014Composer(m, false)
		}},
		{"chat search filters", func() ui.Node { return RenderChatSearch("en-US", ChatSearchView{}) }},
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
