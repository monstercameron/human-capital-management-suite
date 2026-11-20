package chatui

import (
	"regexp"
	"strings"
	"testing"
)

// TestIssue3842_SegmentedChoicesNeverBreakInsideAWord: a choice wraps between
// words only, keeps the width of its longest word, and the group wraps onto
// another line instead of squeezing a choice.
func TestIssue3842_SegmentedChoicesNeverBreakInsideAWord(t *testing.T) {
	rule := func(selector string) string {
		m := regexp.MustCompile(regexp.QuoteMeta(selector) + `\{([^}]*)\}`).FindStringSubmatch(ChatUX048Styles)
		if m == nil {
			t.Fatalf("no rule for %s", selector)
		}
		return m[1]
	}
	choice := rule(".chat-workspace .chattone-reword-choice")
	if strings.Contains(choice, "overflow-wrap:anywhere") || strings.Contains(choice, "break-word") {
		t.Errorf("a choice may break inside a word: %s", choice)
	}
	for _, want := range []string{"word-break:keep-all", "overflow-wrap:normal", "min-inline-size:min-content", "hyphens:none"} {
		if !strings.Contains(choice, want) {
			t.Errorf("choice rule lacks %s: %s", want, choice)
		}
	}
	if seg := rule(".chat-workspace .chattone-reword-seg"); !strings.Contains(seg, "flex-wrap:wrap") || strings.Contains(seg, "flex-wrap:nowrap") {
		t.Errorf("the group does not wrap its choices: %s", seg)
	}
}
