package chatui

import (
	"strings"
	"testing"
)

// Issue 3764: on a phone (or any touch screen) the controls people press are at
// least 44 by 44 CSS pixels: list rows, row and section menus, reply links,
// reactions, author and time buttons, Saved tabs and the thread header buttons.
func TestIssue3764_PhoneTouchTargetsAre44px(t *testing.T) {
	must := []string{
		".chat-workspace .chat-rail .chat-row",
		".chat-workspace .rail-row-more",
		".chat-workspace .section-menu-trigger",
		".chat-workspace .message-stats",
		".chat-workspace .reaction{",
		".chat-workspace .reaction-row>.reaction.add",
		".chat-workspace .message-meta button",
		".chat-workspace .chatsave-seg-button",
		".chat-workspace .thread-pane .side-heading .icon-button",
		".chat-workspace .thread-notify",
		".chat-workspace .chat-skip:focus",
	}
	sheet := Stylesheet
	start := strings.Index(sheet, Issue3764Styles)
	if start < 0 {
		t.Fatal("stylesheet does not carry the touch-target block")
	}
	block := sheet[start:]
	for _, selector := range must {
		at := strings.Index(block, selector)
		if at < 0 {
			t.Errorf("no touch rule for %s", selector)
			continue
		}
		rule := block[at:]
		rule = rule[:strings.Index(rule, "}")]
		if !strings.Contains(rule, "min-block-size:44px") {
			t.Errorf("%s does not reach 44px block size: %s", selector, rule)
		}
	}
	if !strings.Contains(block, ".chat-workspace .rail-row-more{") || !strings.Contains(Issue3764Styles, "min-inline-size:44px") {
		t.Errorf("icon buttons need a 44px inline size as well")
	}
}

func TestIssue3764_ChatRulesWinOverEarlierSizes(t *testing.T) {
	// The reaction chip, Saved tab pills and the row menu are given smaller fixed
	// sizes earlier in the sheet; the touch block must come after them.
	sheet := Stylesheet
	touch := strings.Index(sheet, Issue3764Styles)
	for _, earlier := range []string{".reaction{display:inline-flex", ".chatsave-seg-button{flex:1 1 0", ".section-controls .rail-row-more{inline-size:24px", ".chat-workspace .reaction-row>.reaction.add{position:relative"} {
		at := strings.Index(sheet, earlier)
		if at < 0 {
			continue
		}
		if at > touch {
			t.Errorf("%q comes after the touch block, so it would shrink the target again", earlier)
		}
	}
}
