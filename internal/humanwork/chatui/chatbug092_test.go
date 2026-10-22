package chatui

import (
	"regexp"
	"strings"
	"testing"
)

// TestTodo_CHATBUG_092: after a row menu or Saved was opened, the Archived
// footer was drawn over the last conversation row. Archived is a section in
// the list's own flow now, so in every sidebar state it sits after the rows
// and no stylesheet rule at any width takes it out of the flow.
func TestTodo_CHATBUG_092(t *testing.T) {
	t.Run("the footer sits after the rows in every sidebar state", func(t *testing.T) {
		for _, state := range []struct {
			name string
			menu string
			open bool
		}{
			{"collapsed", "", false},
			{"open", "", true},
			{"menu open", "general", false},
			{"menu open on archived row", "old", true},
		} {
			m := chatbug084Model()
			m.RailMenuID = state.menu
			markup := renderNode(t, rail(m, handlers{local: localUI{archivedOpen: state.open}}))
			foot := strings.Index(markup, `data-section-id="archived"`)
			if foot < 0 || strings.Count(markup, `data-section-id="archived"`) != 1 {
				t.Fatalf("%s: Archived is not drawn exactly once", state.name)
			}
			before, after := markup[:foot], markup[foot:]
			for _, row := range []string{"general", "people-ops"} {
				if !strings.Contains(before, row) {
					t.Errorf("%s: %q is not above Archived", state.name, row)
				}
				// A row menu after the section names its row; only a
				// conversation row under Archived is a leak.
				for _, pair := range []string{
					`chat-rail-row[^>]*data-id="` + row + `"`,
					`data-id="` + row + `"[^>]*chat-rail-row`,
				} {
					if regexp.MustCompile(pair).MatchString(after) {
						t.Errorf("%s: the %q row is under Archived", state.name, row)
					}
				}
			}
			if state.open {
				if !strings.Contains(after, "old-project") {
					t.Errorf("%s: the open section lost its archived row", state.name)
				}
			} else if strings.Contains(after, "chat-rail-row") {
				t.Errorf("%s: the collapsed section draws a row over the list", state.name)
			}
			if scroll := strings.Index(markup, "rail-scroll"); scroll < 0 || scroll > foot {
				t.Errorf("%s: Archived is not inside the scrolling list", state.name)
			}
		}
	})

	t.Run("no rule takes the footer out of the flow", func(t *testing.T) {
		if !strings.Contains(Stylesheet, ".chat-rail{position:relative;display:flex;flex-direction:column") {
			t.Error("the rail is not a vertical flex container")
		}
		if !strings.Contains(Stylesheet, ".chat-rail>.chatstate-archived{flex:none") {
			t.Error("the footer band lost its in-flow flex:none")
		}
		outOfFlow := regexp.MustCompile(`(?i)(position\s*:\s*(absolute|fixed|sticky)|transform\s*:|translate\s*:|margin[a-z-]*\s*:\s*-)`)
		for _, at := range regexp.MustCompile(`(?i)archiv[a-z-]*`).FindAllStringIndex(Stylesheet, -1) {
			// The rule holding the match: from after the previous rule's
			// end to this rule's end; only its declaration block is checked.
			start := strings.LastIndex(Stylesheet[:at[0]], "}") + 1
			body := strings.Index(Stylesheet[start:], "{")
			end := strings.Index(Stylesheet[start:], "}")
			if body < 0 || end < 0 || body > end {
				continue
			}
			rule := Stylesheet[start : start+end+1]
			if outOfFlow.MatchString(rule[strings.Index(rule, "{"):]) {
				t.Errorf("an archived rule leaves the flow: %.120q", rule)
			}
		}
	})
}
