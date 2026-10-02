package chatui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// TestTodo_CHATBUG_029. The picker offered eight emoji: a "Recent" heading with
// nothing under it, "Faces" holding folded hands and eyes, "Symbols" holding
// thumbs up, party and fire, and from the composer it opened at the right edge
// of the page, away from its button. The picker that replaced it (CHATEMOJI-001
// and 002) is held here to this entry's own words: the standard set by
// category with search, Recent only when there are recent emoji, and a place
// beside the button that opened it.
func TestTodo_CHATBUG_029(t *testing.T) {
	m := emojiModel("en-US")
	withEmojiHost(t, m, func(*localUI) {
		ix := chatEmojiData.index

		t.Run("the whole Unicode set", func(t *testing.T) {
			// About 1,900 emoji without skin-tone sequences. The data checked in
			// today is a hand-made subset: the generator's source files
			// (emoji-test.txt and the CLDR annotations) are a download that
			// CHATEMOJI-001 is waiting on. This is the part of the entry that is
			// still open, and the test says so instead of passing on 58.
			if n := len(ix.Set.Entries); n < 1500 {
				t.Skipf("the shipped emoji data holds %d emoji, not the Unicode set; CHATEMOJI-001's source files are needed", n)
			}
		})

		t.Run("Unicode's categories, each emoji in its own", func(t *testing.T) {
			var groups []string
			for _, group := range ix.Set.Groups {
				groups = append(groups, group.Name)
				if group.Count == 0 {
					t.Errorf("the category %s is empty", group.Name)
				}
			}
			if got := strings.Join(groups, " | "); got != "Smileys & Emotion | People & Body | Animals & Nature | Food & Drink | Travel & Places | Activities | Objects | Symbols | Flags" {
				t.Fatalf("categories = %s, want Unicode's nine in Unicode's order", got)
			}
			// The five the old picker had under the wrong heading.
			for glyph, want := range map[string]string{"🙏": "People & Body", "👀": "People & Body", "👍": "People & Body", "🎉": "Activities", "🔥": "Travel & Places", "😂": "Smileys & Emotion"} {
				i, ok := ix.Lookup(glyph)
				if !ok {
					t.Errorf("%s is not in the set", glyph)
					continue
				}
				if got := ix.Set.Groups[ix.Set.Entries[i].Group].Name; got != want {
					t.Errorf("%s is under %s, want %s", glyph, got, want)
				}
			}
		})

		t.Run("search finds an emoji by what it means", func(t *testing.T) {
			for query, want := range map[string]string{"folded hands": "🙏", "eyes": "👀", "party": "🎉", "fire": "🔥", "thumbs up": "👍"} {
				found := ix.Search(query, nil)
				if len(found) == 0 || ix.Set.Entries[found[0]].Glyph != want {
					t.Errorf("searching %q does not lead with %s", query, want)
				}
			}
			st := emojiQuery(emojiOpenState("chat-composer", false, false, false), "party")
			if glyphs := emojiCellGlyphs(t, emojiMarkup(t, m, st, "emoji")); len(glyphs) == 0 || glyphs[0] != "🎉" {
				t.Errorf("the picker, searching party, draws %v", glyphs)
			}
		})

		t.Run("no heading is ever shown empty, and there is none called Recent", func(t *testing.T) {
			for name, prefs := range map[string]emojiPrefs{"before any use": {}, "after some use": emojiPrefs{}.bump("🏆").bump("🏆").bump("🎮")} {
				st := emojiOpenState("chat-composer", false, false, false)
				st.ViewH = 1e6
				v := buildEmojiView(st, ix, prefs)
				heads := 0
				for i, row := range v.rows {
					if !row.Head {
						continue
					}
					heads++
					if i+1 >= len(v.rows) || v.rows[i+1].Head {
						t.Errorf("%s: a heading (group %d) has nothing under it", name, row.Group)
					}
				}
				if heads != 10 || !v.rows[0].Head || v.rows[0].Group != emojiGroupFrequent {
					t.Errorf("%s: %d headings, want Frequently used and the nine categories", name, heads)
				}
			}
			markup := emojiMarkup(t, m, emojiOpenState("chat-composer", false, false, false), "emoji")
			for _, heading := range emojiNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "h3" || emojiHasClass(n, "emoji-pop-heading") }) {
				if text := strings.TrimSpace(emojiText(heading)); text == "Recent" || text == "Faces" || text == "" {
					t.Errorf("the picker has a heading %q", text)
				}
			}
			if strings.Contains(markup, ">Recent<") || strings.Contains(markup, ">Faces<") {
				t.Error("the old picker's headings are still drawn")
			}
			// The first row is what the person uses most, or the starter row.
			if got := strings.Join(emojiPrefs{}.frequent(), " "); got != "👍 ❤️ 😂 🎉 🙏 👀 ✅ 🔥" {
				t.Errorf("the starter row = %s", got)
			}
			if got := strings.Join(emojiPrefs{}.bump("🏆").bump("🏆").bump("🎮").frequent(), " "); got != "🏆 🎮" {
				t.Errorf("frequently used after use = %s, want the person's own", got)
			}
		})
	})

	t.Run("the picker opens beside the button that opened it", func(t *testing.T) {
		column := chatLayerRect{360, 80, 1420, 880}
		button := chatLayerRect{632, 836, 660, 866} // the composer's emoji button
		got := chatEmojiPlace(button, column, 1440, 900, false, false)
		if got.Left != button.left {
			t.Errorf("the picker starts at %v, not at its button (%v); the old one opened at the right edge of the page", got.Left, button.left)
		}
		if gap := button.top - (got.Top + got.Height); gap < 0 || gap > emojiPickerGap+0.001 {
			t.Errorf("the picker ends %v px above its button, want %v", gap, emojiPickerGap)
		}
		if got.Left+got.Width > column.right || got.Left < column.left {
			t.Errorf("the picker leaves the conversation column: %+v", got)
		}
	})
}
