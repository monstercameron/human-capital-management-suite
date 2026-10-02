package chatui

import (
	"strings"
	"testing"
	"unicode/utf16"

	xhtml "golang.org/x/net/html"
)

func emojiGlyphs(items []emojiChoice) []string {
	var out []string
	for _, item := range items {
		out = append(out, item.glyph)
	}
	return out
}

// Typing a colon and two or more letters offers the five best matches with their
// emoji, names and shortcodes; Enter or Tab takes the highlighted one; Escape
// leaves the text; a complete :shortcode: becomes the emoji at Send, except in code.
func TestTodo_CHATEMOJI_003(t *testing.T) {
	t.Run("the trigger is a colon after a space or the start, then two letters", func(t *testing.T) {
		for _, value := range []string{":fi", "hello :fire", "thumbs :thumbs_up", "😀 :pa", ":سر", ":2fa"} {
			q, start, ok := emojiCompletionToken(value, len(utf16.Encode([]rune(value))))
			if value == ":2fa" {
				if ok {
					t.Errorf("a query that starts with a digit opened: %q", q)
				}
				continue
			}
			if !ok || q == "" || start < 0 {
				t.Errorf("%q did not open the list", value)
			}
		}
		for _, value := range []string{":", ":f", "10:30", "10:30pm", "Note: ok", "http://fire", "a:fire", ":fire "} {
			if q, _, ok := emojiCompletionToken(value, len(utf16.Encode([]rune(value)))); ok {
				t.Errorf("%q opened the list with %q", value, q)
			}
		}
	})

	t.Run("the list is the five best matches from the whole set, in the reader's language", func(t *testing.T) {
		for _, tc := range []struct{ locale, query, first, name, code string }{
			{"en-US", "fire", "🔥", "fire", ":fire:"},
			{"en-US", "thumbs_up", "👍", "thumbs up", ":thumbs_up:"},
			{"en-US", "part", "🎉", "party popper", ":party_popper:"},
			{"de-DE", "feuer", "🔥", "Feuer", ":fire:"},
			{"ar", "نار", "🔥", "نار", ":fire:"},
		} {
			m := emojiModel(tc.locale)
			withEmojiHost(t, m, func(*localUI) {
				items := emojiCompletionItems(tc.query)
				if len(items) == 0 || len(items) > 5 || items[0].glyph != tc.first || items[0].label != tc.name || items[0].code != tc.code {
					t.Fatalf("%s %q: %+v, want %s %q %s first and at most five", tc.locale, tc.query, items, tc.first, tc.name, tc.code)
				}
			})
		}
		m := emojiModel("en-US")
		withEmojiHost(t, m, func(*localUI) {
			if got := emojiCompletionItems("face"); len(got) != 5 {
				t.Fatalf("%d suggestions for a broad query, want five", len(got))
			}
			// The person's skin tone and own most-used emoji apply.
			chatEmojiHost.prefs = chatEmojiHost.prefs.bump("🎉")
			chatEmojiHost.prefs.Tone = 3
			if got := emojiGlyphs(emojiCompletionItems("thumbs up")); len(got) == 0 || got[0] != "👍🏽" {
				t.Fatalf("thumbs up in tone 3 = %v", got)
			}
		})
	})

	t.Run("Enter and Tab take the highlighted match, Escape leaves the text", func(t *testing.T) {
		state := composerKeyState{EmojiOpen: true, EmojiHighlighted: true, Draft: ":fire"}
		for _, key := range []string{"Enter", "Tab"} {
			if got := composerKeyAction(state, key); got != composerPickEmoji {
				t.Errorf("%s with the list open picked %v", key, got)
			}
		}
		state.EmojiHighlighted = false
		if got := composerKeyAction(state, "Enter"); got == composerPickEmoji {
			t.Error("Enter picked with nothing highlighted")
		}
		local := localStore{box: &localUI{emojiCompletion: emojiCompletion{Target: "chat-composer", Query: "face", Open: true, Active: 0}}}
		if !emojiCompletionKey(local, "Escape", "chat-composer") || local.get().emojiCompletion.Open {
			t.Fatal("Escape did not close the list")
		}
		local.box.emojiCompletion = emojiCompletion{Target: "chat-composer", Query: "face", Open: true, Active: 0}
		withEmojiHost(t, emojiModel("en-US"), func(*localUI) {
			if !emojiCompletionKey(local, "ArrowDown", "chat-composer") || local.get().emojiCompletion.Active != 1 {
				t.Fatalf("ArrowDown moved to %d", local.get().emojiCompletion.Active)
			}
			if !emojiCompletionKey(local, "ArrowUp", "chat-composer") || local.get().emojiCompletion.Active != 0 {
				t.Fatalf("ArrowUp moved to %d", local.get().emojiCompletion.Active)
			}
		})
	})

	t.Run("a complete shortcode becomes the emoji at Send, except in code", func(t *testing.T) {
		m := emojiModel("en-US")
		withEmojiHost(t, m, func(*localUI) {
			ix := chatEmojiData.index
			for _, tc := range []struct{ in, want string }{
				{"great :thumbs_up: and :fire:", "great 👍 and 🔥"},
				{":tada:" + " no", ":tada: no"}, // not an English name: left alone
				{":party_popper:", "🎉"},
				{"pasted\n:fire:\n:fire:", "pasted\n🔥\n🔥"},
				{"`:fire:` stays", "`:fire:` stays"},
				{"before `:fire:` after :fire:", "before `:fire:` after 🔥"},
				{"``a `:fire:` b`` :fire:", "``a `:fire:` b`` 🔥"},
				{"```\n:fire:\n```\n:fire:", "```\n:fire:\n```\n🔥"},
				{"```go\nx := \":fire:\"\n```", "```go\nx := \":fire:\"\n```"},
				{"```\n:fire: never closed", "```\n:fire: never closed"},
				{"a stray ` backtick :fire:", "a stray ` backtick 🔥"},
				{"Note: ok", "Note: ok"},
				{"at 10:30:45 sharp", "at 10:30:45 sharp"},
				{"a:fire:", "a:fire:"},
				{":notanemoji:", ":notanemoji:"},
				{"::", "::"},
				{":fire", ":fire"},
				{"", ""},
			} {
				if got := emojiShortcodesIn(tc.in, ix, 0); got != tc.want {
					t.Errorf("%q became %q, want %q", tc.in, got, tc.want)
				}
			}
			// The person's skin tone applies, and without data nothing changes.
			if got := emojiShortcodesIn(":thumbs_up:", ix, 3); got != "👍🏽" {
				t.Errorf("tone 3: %q", got)
			}
			if got := emojiShortcodesIn(":fire:", nil, 0); got != ":fire:" {
				t.Errorf("no data: %q", got)
			}
			chatEmojiHost.prefs.Tone = 0
			if got := emojiShortcodesInText("ok :fire:"); got != "ok 🔥" {
				t.Errorf("page wrapper: %q", got)
			}
		})
	})
}

func TestTodo_CHATEMOJI_003_Accessibility(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		t.Run(locale, func(t *testing.T) {
			m := emojiModel(locale)
			withEmojiHost(t, m, func(*localUI) {
				query := map[string]string{"en-US": "fire", "de-DE": "feuer", "ar": "نار"}[locale]
				state := emojiCompletion{Target: "chat-composer", Query: query, Open: true, Active: 0}
				markup := renderNode(t, emojiCompletionMenu(m, state, "chat-composer"))
				lists := emojiNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "role") == "listbox" })
				options := emojiNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "role") == "option" })
				if len(lists) != 1 || chatPolishAttr(lists[0], "aria-label") == "" || len(options) == 0 || len(options) > 5 {
					t.Fatalf("%d lists, %d options", len(lists), len(options))
				}
				selected := 0
				for _, option := range options {
					if chatPolishAttr(option, "aria-label") == "" || chatPolishAttr(option, "aria-label") == strings.TrimSpace(emojiText(option)) {
						t.Errorf("an option has no name of its own: %q", emojiText(option))
					}
					if chatPolishAttr(option, "aria-selected") == "true" {
						selected++
						if !emojiHasClass(option, "active") {
							t.Error("the selected option is not drawn highlighted")
						}
					}
					if !strings.Contains(emojiText(option), ":") {
						t.Errorf("an option does not show its shortcode: %q", emojiText(option))
					}
				}
				if selected != 1 {
					t.Fatalf("%d options selected, want the best match", selected)
				}
				aria := emojiCompletionFieldAria(state, "chat-composer", nil)
				if aria["activedescendant"] != "chat-composer-emoji-option-0" || aria["autocomplete"] != "list" || aria["controls"] != "chat-composer-emoji-completion" {
					t.Fatalf("the field does not point at the list: %v", aria)
				}
				if first := options[0]; !strings.Contains(emojiText(first), "🔥") {
					t.Errorf("the best match for %q is %q", query, emojiText(first))
				}
			})
		})
	}
}

func TestTodo_CHATEMOJI_003_Browser(t *testing.T) {
	chatPolishMatrix(t, func(t *testing.T, m Model, width int, theme string) {
		withEmojiHost(t, m, func(*localUI) {
			state := emojiCompletion{Target: "chat-composer", Query: "face", Open: true, Active: 0}
			menu := chatPolishMarkup(t, emojiCompletionMenu(m, state, "chat-composer"), width, theme)
			rows := emojiNodes(t, menu, func(n *xhtml.Node) bool { return emojiHasClass(n, "mention-option") })
			if len(rows) != 5 {
				t.Fatalf("%d rows for \":face\", want five", len(rows))
			}
			if !emojiHasClass(rows[0], "active") {
				t.Error("the best match is not highlighted")
			}
			for _, row := range rows {
				parts := 0
				for c := row.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == xhtml.ElementNode && c.Data == "span" {
						parts++
					}
				}
				if parts != 3 {
					t.Fatalf("a row has %d parts, want emoji, name and shortcode", parts)
				}
			}
			// A closed list, or one for another field, draws only its empty slot
			// (CHATBUG-086 keeps the slot so the text area does not move).
			for _, closed := range []string{renderNode(t, emojiCompletionMenu(m, emojiCompletion{Active: -1}, "chat-composer")), renderNode(t, emojiCompletionMenu(m, state, "thread-composer"))} {
				if closed != `<div class="emoji-completion-slot"></div>` {
					t.Fatalf("a list is drawn that is not open for this field: %s", closed)
				}
			}
			// The list is the mention list's own style, and the page carries it once.
			if !strings.Contains(menu, `class="mention-menu emoji-completion-menu"`) {
				t.Fatalf("the list is not drawn like the mention list: %s", menu)
			}
			for _, forbidden := range []string{"<style", ` style="`, "⟦", "chat.emoji"} {
				if strings.Contains(menu, forbidden) {
					t.Errorf("list markup contains %q", forbidden)
				}
			}
		})
	})
}
