package chatui

import (
	"strings"
	"testing"
)

// TestTodo_CHATBUG_060 reads the Arabic page's three findings from the styles
// and the markup: the shortcut badge and the room the field keeps for it are on
// the same side, directional icons mirror, and a message body follows the
// parent's direction for its edge.
func TestTodo_CHATBUG_060(t *testing.T) {
	sheet := Stylesheet
	// LTR: the badge is at the inline end (the right) and the field keeps room
	// at its inline end. RTL: the badge is placed at the left, which is the
	// field's inline end, so the same padding is on the same side.
	for _, want := range []string{
		`.rail-search .chat-search-shortcut{position:absolute;inset-inline-end:22px`,
		`.rail-search:has(.chat-search-shortcut) .chat-search{padding-inline-end:64px}`,
		`.chat-workspace[dir="rtl"] .rail-search .chat-search-shortcut{inset-inline-end:auto;left:22px;right:auto}`,
		`.chat-workspace[dir="rtl"] .icon-send,.chat-workspace[dir="rtl"] .icon-reply,.chat-workspace[dir="rtl"] .icon-chevron-right{transform:scaleX(-1)}`,
		`.message-body,.agent-reply-answer{width:100%;max-width:72ch;box-sizing:border-box;margin-inline:0;text-align:match-parent}`,
	} {
		if !strings.Contains(sheet, want) {
			t.Errorf("the stylesheet lacks %q", want)
		}
	}
	// The badge is written left-to-right, which is why its logical inset needs
	// the physical rule above.
	m := chatux014Model(PublicChannel, "general")
	m.Locale, m.Direction = "ar", "rtl"
	if markup := renderNode(t, Build(m)); !strings.Contains(markup, `class="chat-search-shortcut" dir="ltr"`) && !strings.Contains(markup, `dir="ltr" class="chat-search-shortcut"`) {
		t.Errorf("the badge is not written left-to-right: %s", markup[:min(len(markup), 200)])
	}
	if !strings.Contains(sheet, `.chat-workspace[dir="rtl"] :is(.icon-arrow-left,.icon-panel-left,.icon-chevron-left){transform:scaleX(-1)}`) {
		t.Error("the back arrow and the drawer icon do not mirror in right-to-left")
	}
}
