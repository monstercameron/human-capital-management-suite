package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestIssue3398_FilterLabelsSurviveValues: with filters applied, every text,
// date and select control of Refine search still has its own visible label, and
// Before, After and On stay three different words.
func TestIssue3398_FilterLabelsSurviveValues(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		markup, err := ui.RenderToString(RenderChatSearch(locale, ChatSearchView{Query: "budget before:2026-02-01 after:2025-01-01 on:2025-06-01 kind:message"}))
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, id := range []string{"channel", "person", "before", "after", "on", "kind", "written"} {
			start := strings.Index(markup, `for="chatsearch-`+id+`"`)
			end := strings.Index(markup[start+1:], "</label>")
			if start < 0 || end < 0 {
				t.Fatalf("%s: no label for chatsearch-%s", locale, id)
			}
			if !strings.Contains(markup, ` id="chatsearch-`+id+`"`) {
				t.Fatalf("%s: label for chatsearch-%s points at no control", locale, id)
			}
			text := markup[start : start+1+end]
			text = text[strings.Index(text, "<span>")+len("<span>"):]
			text = text[:strings.Index(text, "</span>")]
			if strings.TrimSpace(text) == "" || seen[text] {
				t.Fatalf("%s: label for chatsearch-%s is empty or repeats another: %q", locale, id, text)
			}
			seen[text] = true
		}
	}
}
