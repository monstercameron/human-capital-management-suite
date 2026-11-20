package chatui

import (
	"strings"
	"testing"
)

// issue3760Block is the declaration block of the first rule in the stylesheet
// that starts with selector, found after the marker when one is given.
func issue3760Block(t *testing.T, selector string) string {
	t.Helper()
	i := strings.LastIndex(Stylesheet, selector)
	if i < 0 {
		t.Fatalf("the stylesheet has no rule for %q", selector)
	}
	rest := Stylesheet[i+len(selector):]
	j := strings.Index(rest, "}")
	if j < 0 {
		t.Fatalf("rule for %q is not closed", selector)
	}
	return rest[:j]
}

// TestIssue3760_ModerationHeadingClearsTheAccountStrip: on a phone the
// conversation list keeps the bell and the account menu as a fixed strip at the
// end of its bar. The Moderation page opened from that list starts at the same
// top edge, so its heading leaves room at its end for the strip; the Close
// button is then not under the avatar and receives the tap. The room is the
// list's own (two controls and a gap) and is logical, so it holds in RTL.
func TestIssue3760_ModerationHeadingClearsTheAccountStrip(t *testing.T) {
	marker := `.chat-workspace[data-sidebar-open="true"] .chatremove-overlay-page .chatmod005-heading.chat-panel-head{`
	i := strings.Index(Stylesheet, "@media(max-width:760px){"+marker)
	if i < 0 {
		t.Fatal("the Moderation heading has no phone rule that clears the account strip")
	}
	rule := issue3760Block(t, marker)
	if !strings.Contains(rule, "padding-inline-end:calc(2 * var(--hcm-control-height) + var(--hcm-space-1))") {
		t.Fatalf("the heading does not leave the strip's width at its end: %s", rule)
	}
	if strings.Contains(rule, "padding-right") || strings.Contains(rule, "padding-left") {
		t.Fatalf("the room is not logical, so RTL would be wrong: %s", rule)
	}
	// The same width the list's own heading leaves for the strip.
	if !strings.Contains(Stylesheet, `.chat-workspace[data-sidebar-open="true"] .rail-head{padding-inline-end:calc(2 * var(--hcm-control-height) + var(--hcm-space-1))}`) {
		t.Fatal("the list heading no longer leaves room for the strip; keep the two in step")
	}
}

// TestIssue3762_ModerationTabsStayOnOneLine: a tab's label and its count never
// wrap (a long German word, an Arabic phrase, a count of Arabic digits), the pill
// grows to hold its buttons, and when the tabs are wider than the screen the row
// scrolls sideways instead of breaking a word.
func TestIssue3762_ModerationTabsStayOnOneLine(t *testing.T) {
	row := issue3760Block(t, ":root .chatremove .chatmod005-tabs.chatsave-seg{")
	for _, want := range []string{"max-inline-size:100%", "block-size:auto", "overflow-x:auto", "flex-wrap:nowrap"} {
		if !strings.Contains(row, want) {
			t.Errorf("the tab row lacks %q: %s", want, row)
		}
	}
	button := issue3760Block(t, ":root .chatremove .chatmod005-tabs .chatmod005-tab.chatsave-seg-button{")
	for _, want := range []string{"flex:1 0 auto", "white-space:nowrap", "overflow-wrap:normal", "word-break:normal"} {
		if !strings.Contains(button, want) {
			t.Errorf("a tab lacks %q: %s", want, button)
		}
	}
	count := issue3760Block(t, ":root .chatremove .chatmod005-tabs .chatsave-seg-count{")
	if !strings.Contains(count, "white-space:nowrap") || !strings.Contains(count, "unicode-bidi:isolate") {
		t.Errorf("a tab's count can still wrap or mix with the label: %s", count)
	}
}

// TestIssue3762_TabLabelsAreOneUnbrokenWord: the three tab labels are rendered
// as one text node each in every language (nothing inserts a break), so the CSS
// above is all that decides where a line may end.
func TestIssue3762_TabLabelsAreOneUnbrokenWord(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, key := range []string{"tab_open", "tab_resolved", "tab_permissions"} {
			label := chatremoveText(locale, key)
			if label == "" || strings.ContainsAny(label, "\n­") {
				t.Errorf("%s %s: label %q is empty or carries a break", locale, key, label)
			}
		}
	}
}
