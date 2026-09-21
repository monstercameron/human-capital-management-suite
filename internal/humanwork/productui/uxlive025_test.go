package productui

import (
	"strings"
	"testing"
)

// UXLIVE-025's RED was measured on the running server and then reproduced
// exactly: on Brand & appearance at 1440 x 900 the root box reports
// scrollHeight 1207 while html and body are overflow:hidden, and
// window.scrollTo(0, 500) moves the document to 307 and puts the global
// header at top -307. overflow:hidden still creates a scroll container, so
// the document is scrollable by script -- a focus scroll-into-view, an
// anchor jump, find-in-page -- while offering no scrollbar to scroll back.
// That is the shell disappearing with no way to recover it.
//
// overflow:clip clips the same content and creates no scroll container at
// all, so the root cannot be scrolled by any means. The shell's own scroll
// owners (main and the sidebar) are unaffected.

// TestTodo_UXLIVE_025 is the primary red/green test: the root is clipped,
// not merely scrollbar-less.
func TestTodo_UXLIVE_025(t *testing.T) {
	sheet := Stylesheet()

	rule := lastRuleFor(sheet, "html,body,#app")
	if rule == "" {
		t.Fatalf("the root sizing rule is gone; this todo's premise no longer holds")
	}
	if !strings.Contains(rule, "overflow:clip") {
		t.Fatalf("the root still creates a scroll container it offers no scrollbar for: %q", rule)
	}

	// The fallback stays for engines without overflow:clip.
	if !strings.Contains(sheet, "html,body,#app{") || !strings.Contains(sheet, "overflow:hidden") {
		t.Fatalf("the hidden fallback was dropped entirely")
	}

	// Clipping the root is not enough on its own: a descendant that
	// positions itself absolutely resolves against the initial containing
	// block unless its scroll owner is one, escapes the clip and extends the
	// document. The scroll owners establish that containing block.
	owners := lastRuleFor(sheet, ".main,.main-scroll,.sidebar")
	if !strings.Contains(owners, "position:relative") {
		t.Fatalf("the scroll owners are not containing blocks, so absolutely positioned descendants still escape them: %q", owners)
	}
}

// TestTodo_UXLIVE_025_Browser keeps the shell's real scroll owners working:
// clipping the root must not clip the regions that are supposed to scroll.
func TestTodo_UXLIVE_025_Browser(t *testing.T) {
	sheet := Stylesheet()
	for _, owner := range []string{".main-scroll", ".sidebar"} {
		if !strings.Contains(sheet, owner) {
			t.Fatalf("the sheet no longer styles the scroll owner %q", owner)
		}
	}
	// Printing still escapes the clip, so a printed page is not one screen.
	if !strings.Contains(sheet, "overflow:visible!important") {
		t.Fatalf("the print escape from the root clip was lost")
	}
}

// lastRuleFor returns the declarations of the last rule whose selector is
// exactly sel, which is the one that wins on source order.
func lastRuleFor(sheet, sel string) string {
	out := ""
	for _, chunk := range strings.Split(sheet, "}") {
		open := strings.Index(chunk, "{")
		if open < 0 {
			continue
		}
		if strings.TrimSpace(chunk[:open]) == sel {
			out = strings.TrimSpace(chunk[open+1:])
		}
	}
	return out
}
