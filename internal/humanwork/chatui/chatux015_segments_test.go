package chatui

import (
	"regexp"
	"strings"
	"testing"
)

// TestTodo_CHATUX_015_ModerationSegments: Open and Resolved are the one
// segmented control Saved uses, each stating its count.
func TestTodo_CHATUX_015_ModerationSegments(t *testing.T) {
	page := renderNode(t, ModerationPage(ModerationPageModel{Locale: "en-US", State: StateReady, OpenCount: 3, ResolvedCount: 12, ResolvedKnown: true}))
	for _, want := range []string{
		`class="chatsave-seg chatmod005-tabs"`, `role="tablist"`,
		`<span>Open</span><span class="chatsave-seg-count">3</span>`,
		`<span>Resolved</span><span class="chatsave-seg-count">12</span>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the Moderation control lacks %q: %s", want, page)
		}
	}
	if strings.Count(page, `aria-selected="true"`) != 1 || !regexp.MustCompile(`aria-selected="true"[^>]*><span>Open<`).MatchString(page) {
		t.Errorf("exactly one segment is selected, and it is Open: %s", page)
	}
	// An unknown count is left off, not shown as zero.
	page = renderNode(t, ModerationPage(ModerationPageModel{Locale: "en-US", State: StateReady, Tab: "resolved", OpenCount: 0}))
	if !strings.Contains(page, `<span>Open</span><span class="chatsave-seg-count">0</span>`) || strings.Contains(page, `<span>Resolved</span><span`) {
		t.Errorf("counts: %s", page)
	}
	// Arabic digits and the page's own direction.
	page = renderNode(t, ModerationPage(ModerationPageModel{Locale: "ar", State: StateReady, OpenCount: 4, ResolvedCount: 1, ResolvedKnown: true}))
	if !strings.Contains(page, `chatsave-seg-count">٤<`) || !strings.Contains(page, `dir="rtl"`) {
		t.Errorf("ar: %s", page)
	}
	if !strings.Contains(chatux015Styles, ".chatmod005-tab.chatsave-seg-button") {
		t.Error("the segmented control has no style of its own on the Moderation page")
	}
}
