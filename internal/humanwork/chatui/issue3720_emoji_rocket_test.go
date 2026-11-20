package chatui

import (
	"strings"
	"testing"
)

// Issue 3720: the demo data reacts with a rocket, but the picker had no rocket to
// find or choose.
func TestIssue3720_RocketIsFindableAndSelectableInEveryLanguage(t *testing.T) {
	for _, c := range []struct{ locale, query string }{
		{"en-US", "rocket"},
		{"de-DE", "rakete"},
		{"ar", "صاروخ"},
	} {
		m := emojiModel(c.locale)
		withEmojiHost(t, m, func(*localUI) {
			st := emojiOpenState("chat-composer", true, false, false)
			st.Query = c.query
			markup := emojiMarkup(t, m, st, "reaction")
			if strings.Contains(markup, "emoji-pop-note") {
				t.Errorf("%s: searching %q found nothing", c.locale, c.query)
			}
			glyphs := emojiCellGlyphs(t, markup)
			if len(glyphs) == 0 || glyphs[0] != "🚀" {
				t.Errorf("%s: searching %q gave %v, want the rocket first", c.locale, c.query, glyphs)
			}
		})
	}
}

func TestIssue3720_RocketIsInTheBrowsableGrid(t *testing.T) {
	m := emojiModel("en-US")
	withEmojiHost(t, m, func(*localUI) {
		st := emojiOpenState("chat-composer", true, false, false)
		st.ViewH = 1e6
		markup := emojiMarkup(t, m, st, "reaction")
		found := false
		for _, g := range emojiCellGlyphs(t, markup) {
			found = found || g == "🚀"
		}
		if !found {
			t.Fatal("the rocket is not among the emoji the picker draws")
		}
	})
}
