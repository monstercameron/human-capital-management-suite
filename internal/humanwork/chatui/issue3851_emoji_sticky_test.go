package chatui

import (
	"strconv"
	"testing"

	xhtml "golang.org/x/net/html"
)

// Issue 3851: after a category tab was chosen, the sticky heading and the marked
// tab named the category before it.

func issue3851Sticky(t *testing.T, markup string) string {
	t.Helper()
	spans := emojiNodes(t, markup, func(n *xhtml.Node) bool {
		return n.Data == "span" && n.Parent != nil && emojiHasClass(n.Parent, "emoji-pop-sticky")
	})
	if len(spans) != 1 {
		t.Fatalf("%d sticky headings, want 1", len(spans))
	}
	return emojiText(spans[0])
}

func issue3851CurrentTab(t *testing.T, markup string) string {
	t.Helper()
	tabs := emojiNodes(t, markup, func(n *xhtml.Node) bool {
		return n.Data == "button" && emojiHasClass(n, "emoji-pop-tab") && emojiHasClass(n, "current")
	})
	if len(tabs) != 1 {
		t.Fatalf("%d marked tabs, want 1", len(tabs))
	}
	return chatPolishAttr(tabs[0], "aria-label")
}

// issue3851MaxScroll is how far the drawn content lets the grid scroll: the
// spacers, the rows in the page and anything after them, less the viewport.
func issue3851MaxScroll(t *testing.T, markup string, st emojiPickerState, v emojiView) float64 {
	t.Helper()
	height := 0.0
	for _, n := range emojiNodes(t, markup, func(n *xhtml.Node) bool { return emojiHasClass(n, "emoji-pop-spacer") }) {
		h, _ := strconv.Atoi(chatPolishAttr(n, "data-h"))
		height += float64(h)
	}
	for _, n := range emojiNodes(t, markup, func(n *xhtml.Node) bool { return emojiHasClass(n, "emoji-pop-headrow") }) {
		_ = n
		height += v.metrics.Head
	}
	for _, n := range emojiNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "div" && emojiHasClass(n, "emoji-pop-row") }) {
		_ = n
		height += v.metrics.Cell
	}
	viewH := st.ViewH
	if viewH <= 0 {
		viewH = emojiDefaultViewH
	}
	return max(0, height-viewH)
}

func TestIssue3851_StickyHeadingNamesTheCategoryAtTheTop(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := emojiModel(locale)
		withEmojiHost(t, m, func(*localUI) {
			st := emojiOpenState("chat-composer", false, false, false)
			v := buildEmojiView(st, chatEmojiData.index, emojiPrefs{})
			animals := emojiHeadRow(v.rows, 2)
			if animals < 0 {
				t.Fatal("no Animals & Nature heading")
			}
			head := v.rows[animals]
			// A browser lands on a device pixel, so a grid scrolled to a heading can
			// report a position a fraction before it. The heading hidden under the
			// sticky label is the category being shown.
			for _, offset := range []float64{0, -0.8, -0.2, 0.4, v.metrics.Head - 1} {
				at := st
				at.Scroll = head.Y + offset
				markup := emojiMarkup(t, m, at, "emoji")
				want := chatEmojiGroupName(m, v, 2)
				if got := issue3851Sticky(t, markup); got != want {
					t.Errorf("%s scroll %+.1f from the heading: sticky heading %q, want %q", locale, offset, got, want)
				}
				if got := issue3851CurrentTab(t, markup); got != want {
					t.Errorf("%s scroll %+.1f from the heading: marked tab %q, want %q", locale, offset, got, want)
				}
			}
		})
	}
}

func TestIssue3851_EveryCategoryTabLeavesItsOwnHeadingShowing(t *testing.T) {
	for _, touch := range []bool{false, true} {
		m := emojiModel("en-US")
		withEmojiHost(t, m, func(*localUI) {
			open := emojiOpenState("chat-composer", false, touch, false)
			v := buildEmojiView(open, chatEmojiData.index, emojiPrefs{})
			for group := -1; group < len(v.set.Groups); group++ {
				if emojiHeadRow(v.rows, group) < 0 {
					continue
				}
				st := emojiCategory(open, v, group)
				// The browser can only scroll as far as the content allows; draw at the
				// requested position, then at the position the content lets it reach.
				markup := emojiMarkup(t, m, st, "emoji")
				if limit := issue3851MaxScroll(t, markup, st, v); st.Scroll > limit {
					st.Scroll = limit
				}
				markup = emojiMarkup(t, m, st, "emoji")
				want := chatEmojiGroupName(m, v, group)
				if got := issue3851Sticky(t, markup); got != want {
					t.Errorf("touch=%v tab for %q: sticky heading %q", touch, want, got)
				}
				if got := issue3851CurrentTab(t, markup); got != want {
					t.Errorf("touch=%v tab for %q: marked tab %q", touch, want, got)
				}
			}
		})
	}
}
