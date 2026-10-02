package chatui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"

	"github.com/monstercameron/GoWebComponents/v5/html"
)

// withFlagsDrawn runs fn as a platform that does (or does not) draw flag emoji.
func withFlagsDrawn(drawn bool, fn func()) {
	saved := emojiFlagsDrawn
	defer func() { emojiFlagsDrawn = saved }()
	emojiFlagsDrawn = func() bool { return drawn }
	fn()
}

func mustLookupEmoji(t *testing.T, glyph string) int {
	t.Helper()
	i, ok := chatEmojiData.index.Lookup(glyph)
	if !ok {
		t.Fatalf("%s is not in the emoji data", glyph)
	}
	return i
}

// A platform with no flag glyphs shows a country flag as two letters in a box. The
// picker leaves the Flags group out, flags leave Frequently used, search and the
// one-click reactions, and a flag that is already in a reaction or a message is a
// labelled chip: the country code, with the country's name as its accessible name.
func TestTodo_CHATBUG_043(t *testing.T) {
	t.Run("what is a flag the platform may not draw", func(t *testing.T) {
		for glyph, want := range map[string]bool{"🇺🇸": true, "🇩🇪": true, "\U0001F3F4\U000E0067\U000E0062\U000E0065\U000E006E\U000E0067\U000E007F": true, "🏁": false, "🏳️": false, "🚩": false, "👍": false, "🇺": false, "": false} {
			if got := emojiNeedsFlagGlyphs(glyph); got != want {
				t.Errorf("emojiNeedsFlagGlyphs(%q) = %v, want %v", glyph, got, want)
			}
		}
		if emojiFlagCode("🇺🇸") != "US" || emojiFlagCode("🇯🇵") != "JP" || emojiFlagCode("🏁") != "" {
			t.Fatal("country codes are read wrongly")
		}
		var joined []string
		for _, s := range emojiSplitFlags("I live in 🇺🇸 not 🇩🇪🇫🇷 ok") {
			joined = append(joined, map[bool]string{true: "[", false: "<"}[s.Flag]+s.Text)
		}
		if got := strings.Join(joined, "|"); got != "<I live in |[🇺🇸|< not |[🇩🇪|[🇫🇷|< ok" {
			t.Fatalf("segments = %s", got)
		}
		if emojiSplitFlags("no flags here 👍🏁") != nil {
			t.Fatal("text without a country flag was cut")
		}
		if got := strings.Join(emojiWithoutFlags([]string{"🇺🇸", "👍", "🏁", "🇩🇪"}), " "); got != "👍 🏁" {
			t.Fatalf("without flags: %q", got)
		}
	})

	t.Run("the picker leaves Flags out where flags cannot be drawn", func(t *testing.T) {
		m := emojiModel("en-US")
		withEmojiHost(t, m, func(*localUI) {
			chatEmojiHost.prefs = emojiPrefs{}.bump("🇺🇸").bump("🇺🇸").bump("🔥")
			for _, drawn := range []bool{true, false} {
				tabs := map[bool]int{true: 10, false: 9}[drawn]
				withFlagsDrawn(drawn, func() {
					st := emojiOpenState("chat-composer", false, false, false)
					st.ViewH = 1e6
					markup := emojiMarkup(t, m, st, "emoji")
					if got := len(emojiNodes(t, markup, func(n *xhtml.Node) bool { return emojiHasClass(n, "emoji-pop-tab") })); got != tabs {
						t.Errorf("drawn=%v: %d category tabs, want %d", drawn, got, tabs)
					}
					hasFlag := false
					for _, glyph := range emojiCellGlyphs(t, markup) {
						hasFlag = hasFlag || emojiNeedsFlagGlyphs(glyph)
					}
					if hasFlag != drawn || strings.Contains(markup, ">Flags<") != drawn {
						t.Errorf("drawn=%v: flag cells %v, Flags heading %v", drawn, hasFlag, strings.Contains(markup, ">Flags<"))
					}
					v := buildEmojiView(st, chatEmojiData.index, chatEmojiHost.prefs)
					if cell, _ := v.cell(v.seq.len() - 1); emojiNeedsFlagGlyphs(cell.Glyph) != drawn {
						t.Errorf("drawn=%v: the last emoji in reach is %q", drawn, cell.Glyph)
					}
					// Search: no flag in a result list, and nothing else lost.
					found := buildEmojiView(emojiQuery(st, "flag"), chatEmojiData.index, chatEmojiHost.prefs)
					for pos := 0; pos < found.seq.len(); pos++ {
						if cell, _ := found.cell(pos); !drawn && emojiNeedsFlagGlyphs(cell.Glyph) {
							t.Errorf("a flag turned up in a result list: %q", cell.Glyph)
						}
					}
					if want := map[bool]int{true: 6, false: 0}[drawn]; found.seq.len() != want {
						t.Errorf("drawn=%v: \"flag\" found %d emoji, want %d", drawn, found.seq.len(), want)
					}
					// Frequently used and the message bar's one-click reactions.
					quick := chatEmojiQuickReactions()
					if len(quick) != 3 || strings.Contains(strings.Join(quick, ""), "🇺🇸") != drawn {
						t.Errorf("drawn=%v: quick reactions %v", drawn, quick)
					}
					frequent := emojiCellGlyphs(t, emojiMarkup(t, m, emojiOpenState("chat-composer", false, false, false), "emoji"))
					if len(frequent) < 2 || strings.Contains(strings.Join(frequent[:2], ""), "🇺🇸") != drawn {
						t.Errorf("drawn=%v: Frequently used starts %v", drawn, frequent[:min(2, len(frequent))])
					}
				})
			}
			// Only flags used so far: the starter row stands in rather than an empty heading.
			chatEmojiHost.prefs = emojiPrefs{}.bump("🇺🇸")
			withFlagsDrawn(false, func() {
				got := emojiCellGlyphs(t, emojiMarkup(t, m, emojiOpenState("chat-composer", false, false, false), "emoji"))
				if len(got) < len(emojiStarter) || strings.Join(got[:len(emojiStarter)], " ") != strings.Join(emojiStarter, " ") {
					t.Errorf("flags-only history: Frequently used = %v", got[:min(8, len(got))])
				}
			})
		})
	})

	t.Run("the flags group is last in the shipped data, so leaving it out leaves the rest in place", func(t *testing.T) {
		withEmojiHost(t, emojiModel("en-US"), func(*localUI) {
			set := chatEmojiData.index.Set
			last, ok := emojiFlagsGroup(set)
			if !ok || set.Groups[last].First+set.Groups[last].Count != len(set.Entries) {
				t.Fatalf("the Flags group is not the last: %+v", set.Groups[len(set.Groups)-1])
			}
		})
	})

	t.Run("a flag in a reaction or a message is a labelled chip", func(t *testing.T) {
		for _, locale := range []string{"en-US", "de-DE", "ar"} {
			m := emojiModel(locale)
			m.Callbacks.ReactWith = func(string, string) {}
			msg := Message{ID: "post", Body: "Zuhause 🇩🇪 und 🇺🇸", Chips: []ReactionChip{{Emoji: "🇺🇸", Count: 3}, {Emoji: "👍", Count: 1}}}
			withEmojiHost(t, m, func(*localUI) {
				name := chatEmojiData.index.Name(mustLookupEmoji(t, "🇺🇸"))
				withFlagsDrawn(false, func() {
					row := renderNode(t, reactionRow(m, msg))
					chips := emojiNodes(t, row, func(n *xhtml.Node) bool { return emojiHasClass(n, "emoji-flag-chip") })
					if len(chips) != 1 || chatPolishAttr(chips[0], "aria-label") != name || chatPolishAttr(chips[0], "title") != name || chatPolishAttr(chips[0], "role") != "img" || emojiText(chips[0]) != "US" {
						t.Fatalf("%s: reaction chip: %d chips: %s", locale, len(chips), row)
					}
					buttons := emojiNodes(t, row, func(n *xhtml.Node) bool {
						return n.Data == "button" && emojiHasClass(n, "reaction") && chatPolishAttr(n, "data-emoji") == "🇺🇸"
					})
					if len(buttons) != 1 || !strings.Contains(chatPolishAttr(buttons[0], "aria-label"), name) {
						t.Errorf("%s: the reaction button is not named for the country", locale)
					}
					body := renderNode(t, html.Div(html.Props{}, markdownMessageBody(m, msg.Body)...))
					if got := len(emojiNodes(t, body, func(n *xhtml.Node) bool { return emojiHasClass(n, "emoji-flag-chip") })); got != 2 || strings.Contains(body, "🇩🇪") {
						t.Errorf("%s: message text drew %d flag chips (flags left in text: %v)", locale, got, strings.Contains(body, "🇩🇪"))
					}
				})
				withFlagsDrawn(true, func() {
					if row := renderNode(t, reactionRow(m, msg)); strings.Contains(row, "emoji-flag-chip") || !strings.Contains(row, "🇺🇸") {
						t.Errorf("%s: a platform that draws flags got a chip", locale)
					}
				})
			})
		}
	})

	t.Run("before the data has loaded the chip names the country code", func(t *testing.T) {
		m := emojiModel("en-US")
		withEmojiHost(t, m, func(*localUI) {
			chatEmojiData = chatEmojiDataStore{status: emojiDataLoading}
			withFlagsDrawn(false, func() {
				chip := renderNode(t, emojiFlagChip(m, "🇯🇵"))
				if !strings.Contains(chip, `aria-label="Flag: JP"`) || !strings.Contains(chip, ">JP<") {
					t.Fatalf("chip with no data: %s", chip)
				}
			})
		})
	})
}

// The same, on the whole page and in the three languages.
func TestTodo_CHATBUG_043_Browser(t *testing.T) {
	chatPolishMatrix(t, func(t *testing.T, m Model, width int, theme string) {
		m.Callbacks.ReactWith = func(string, string) {}
		withEmojiHost(t, m, func(*localUI) {
			withFlagsDrawn(false, func() {
				m.Messages = []Message{{ID: "post", Author: "Alex", Body: "Flag 🇫🇷", Chips: []ReactionChip{{Emoji: "🇯🇵", Count: 2, Mine: true}}}}
				page := chatPolishMarkup(t, Build(m), width, theme)
				if got := strings.Count(page, `class="emoji-flag-chip"`); got < 2 {
					t.Fatalf("the page shows %d flag chips for a flag in a reaction and one in text", got)
				}
				for _, bad := range []string{"⟦", ">🇯🇵<", ">🇫🇷<"} {
					if strings.Contains(page, bad) {
						t.Errorf("the page carries %q", bad)
					}
				}
			})
		})
	})
}
