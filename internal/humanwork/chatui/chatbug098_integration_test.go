package chatui

import (
	"strings"
	"testing"
)

// TestTodo_CHATBUG_098_Integration folds the Archived section through its
// states together with the row menu, search and Chat preferences: every
// section stays in view in every combination, the archived rows appear only
// while open, and the section keeps its place at the end of the scrolling
// list.
func TestTodo_CHATBUG_098_Integration(t *testing.T) {
	for _, state := range []struct {
		name   string
		menu   string
		open   bool
		search string
	}{
		{"collapsed", "", false, ""},
		{"open", "", true, ""},
		{"open with menu", "general", true, ""},
		{"open with menu on archived row", "old", true, ""},
		{"open while searching", "", true, "holiday"},
		{"collapsed with menu while searching", "general", false, "policy"},
	} {
		m := chatbug084Model()
		m.RailMenuID = state.menu
		m.Search = state.search
		markup := renderNode(t, rail(m, handlers{local: localUI{archivedOpen: state.open}}))
		// Every section of the list is in view: folding Archived never
		// takes a section with it.
		for _, section := range []string{`data-section-id="channels"`, `data-section-id="direct"`, `data-section-id="archived"`} {
			if !strings.Contains(markup, section) {
				t.Errorf("%s: %s left the rail", state.name, section)
			}
		}
		foot := strings.Index(markup, `data-section-id="archived"`)
		channels := strings.Index(markup, `data-section-id="channels"`)
		direct := strings.Index(markup, `data-section-id="direct"`)
		if foot < channels || foot < direct {
			t.Errorf("%s: Archived is not the last section", state.name)
		}
		if state.open {
			if !strings.Contains(markup[foot:], "old-project") {
				t.Errorf("%s: the open section lost its archived row", state.name)
			}
		} else if strings.Contains(markup[foot:], "chat-rail-row") {
			t.Errorf("%s: the collapsed section draws rows over the list", state.name)
		}
		// Chat preferences rides the rail head as a top-layer popover, not
		// as a row or a band in the list.
		if !strings.Contains(markup, `data-chat-layer="chat-prefs"`) {
			t.Errorf("%s: Chat preferences left the rail head", state.name)
		}
		if prefs := strings.Index(markup, `data-chat-layer="chat-prefs"`); prefs > strings.Index(markup, "rail-scroll") {
			t.Errorf("%s: Chat preferences sits inside the scrolling list", state.name)
		}
	}
}
