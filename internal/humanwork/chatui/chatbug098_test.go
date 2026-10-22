package chatui

import (
	"regexp"
	"strings"
	"testing"
)

// TestTodo_CHATBUG_098: archived conversations are a normal section at the
// end of the sidebar list, inside its scroll: each row carries the quiet
// Archived tag and the row menu leads with Open and Restore. No band, box or
// footer is positioned over rows in any sidebar state.
func TestTodo_CHATBUG_098(t *testing.T) {
	t.Run("archived rows carry the quiet tag with Open and Restore first", func(t *testing.T) {
		m := chatbug084Model()
		m.RailMenuID = "old"
		markup := renderNode(t, rail(m, handlers{local: localUI{archivedOpen: true}}))
		foot := strings.Index(markup, `data-section-id="archived"`)
		if foot < 0 {
			t.Fatalf("Archived section missing: %s", markup)
		}
		after := markup[foot:]
		if !strings.Contains(after, "old-project") || !strings.Contains(after, "chatstate-badge") || !strings.Contains(after, chatstateText(m, "archived")) {
			t.Error("an archived row lost its quiet Archived tag")
		}
		for _, want := range []string{
			`.chatbug092-archived .chat-row .chat-row-name{color:var(--muted)}`,
			`.chatbug092-archived .chat-row>.chatstate-badge{background:none;border:0`,
		} {
			if !strings.Contains(Stylesheet, want) {
				t.Errorf("the quiet tag lost its rule %q", want)
			}
		}
		menu := markup[strings.Index(markup, "rail-row-menu"):]
		open, restore := strings.Index(menu, chatstateText(m, "open_channel")), strings.Index(menu, chatstateText(m, "restore"))
		if open < 0 || restore < 0 || restore < open {
			t.Fatalf("the archived row menu does not lead with Open and Restore: %s", menu)
		}
		if item := strings.Index(menu, "menu-item"); item < 0 || item > open {
			t.Fatalf("Open is not the first menu item: %s", menu)
		}
	})

	t.Run("no band box or footer sits over rows in any state", func(t *testing.T) {
		for _, state := range []struct {
			name   string
			menu   string
			open   bool
			search string
		}{
			{"collapsed", "", false, ""},
			{"open", "", true, ""},
			{"menu open", "general", false, ""},
			{"menu open on archived row", "old", true, ""},
			{"searching", "", true, "holiday"},
			{"searching with menu", "general", false, "holiday"},
		} {
			m := chatbug084Model()
			m.RailMenuID = state.menu
			m.Search = state.search
			markup := renderNode(t, rail(m, handlers{local: localUI{archivedOpen: state.open}}))
			if strings.Contains(markup, "chatstate-archived") {
				t.Errorf("%s: the fixed footer band is back in the rail", state.name)
			}
			foot := strings.Index(markup, `data-section-id="archived"`)
			if foot < 0 {
				t.Errorf("%s: Archived section missing", state.name)
				continue
			}
			if scroll := strings.Index(markup, "rail-scroll"); scroll < 0 || scroll > foot {
				t.Errorf("%s: Archived is not inside the scrolling list", state.name)
			}
			for _, row := range []string{"general", "people-ops"} {
				if !strings.Contains(markup[:foot], row) {
					t.Errorf("%s: %q is not above Archived", state.name, row)
				}
			}
			// The row menu is a top-layer popover, never an in-flow box.
			if state.menu != "" {
				menu := markup[strings.Index(markup, "rail-row-menu"):]
				if !strings.Contains(menu, `popover="manual"`) {
					t.Errorf("%s: the row menu is not a top-layer popover", state.name)
				}
			}
		}
	})

	t.Run("no archived or footer rule leaves the flow", func(t *testing.T) {
		if !strings.Contains(Stylesheet, ".rail-fixed{flex:none") {
			t.Error("the fixed rows lost their in-flow flex:none")
		}
		outOfFlow := regexp.MustCompile(`(?i)(position\s*:\s*(absolute|fixed|sticky)|transform\s*:|translate\s*:|margin[a-z-]*\s*:\s*-)`)
		for _, at := range regexp.MustCompile(`(?i)(archiv[a-z-]*|rail-fixed|chatstate-archived)`).FindAllStringIndex(Stylesheet, -1) {
			start := strings.LastIndex(Stylesheet[:at[0]], "}") + 1
			body := strings.Index(Stylesheet[start:], "{")
			end := strings.Index(Stylesheet[start:], "}")
			if body < 0 || end < 0 || body > end {
				continue
			}
			rule := Stylesheet[start : start+end+1]
			if outOfFlow.MatchString(rule[strings.Index(rule, "{"):]) {
				t.Errorf("an archived/footer rule leaves the flow: %.120q", rule)
			}
		}
	})
}
