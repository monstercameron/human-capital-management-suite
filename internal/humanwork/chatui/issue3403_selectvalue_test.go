package chatui

import (
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestIssue3403_SearchSelectsCarryTheirValueForTheBrowserSync pins the live
// browser defect: when the results arrive the reconciler rebuilds the option
// nodes of the Refine search selects without applying the selected flag, and
// the browser then leaves the last option (Hindi) chosen. The selects publish
// their rendered value as data-chat-select-value so the field sync re-applies it.
func TestIssue3403_SearchSelectsCarryTheirValueForTheBrowserSync(t *testing.T) {
	cases := []struct {
		query, kind, written string
	}{
		{"budget", "", ""},
		{`budget lang:"en" kind:"message"`, "message", "en"},
	}
	for _, c := range cases {
		markup, err := ui.RenderToString(RenderChatSearch("en-US", ChatSearchView{Query: c.query}))
		if err != nil {
			t.Fatal(err)
		}
		for id, want := range map[string]string{"chatsearch-kind": c.kind, "chatsearch-written": c.written} {
			got, ok := issue3690Attr(issue3690Element(t, markup, id), "data-chat-select-value")
			if !ok || got != want {
				t.Fatalf("%s for %q: data-chat-select-value=%q present=%v, want %q", id, c.query, got, ok, want)
			}
		}
	}
}
