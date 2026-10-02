package chatui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

func chatbugHasAncestorClass(n *xhtml.Node, class string) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == xhtml.ElementNode && chatPolishHasClass(p, class) {
			return true
		}
	}
	return false
}

// TestTodo_CHATBUG_003 pins the formatting toolbar: every tool is its own
// icon button in one row. CHATUX-004 changed this test: the narrow Formatting
// menu, its trigger and the "Markdown" word are gone, so the row is the only
// form and the Aa button shows or hides it.
func TestTodo_CHATBUG_003(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := Model{Locale: locale}
		markup := renderNode(t, formatToolbar(m, "chat-composer", false))
		if strings.Contains(markup, "<details") || strings.Contains(markup, "<summary") {
			t.Fatalf("%s: a raw details/summary toolbar: %s", locale, markup)
		}
		buttons := chatPolishNodes(t, markup, func(n *xhtml.Node) bool {
			return n.Data == "button" && chatPolishAttr(n, "data-action") == "format"
		})
		inline, menu := 0, 0
		for _, b := range buttons {
			if chatPolishAttr(b, "disabled") != "" || chatPolishAttr(b, "title") == "" || chatPolishAttr(b, "aria-label") == "" {
				t.Fatalf("%s: %s button is disabled or unlabelled", locale, chatPolishAttr(b, "data-extra"))
			}
			switch {
			case chatbugHasAncestorClass(b, "format-inline"):
				inline++
			default:
				t.Fatalf("%s: format button outside the row", locale)
			}
		}
		if inline != len(composerFormats) || menu != 0 {
			t.Fatalf("%s: %d row buttons and %d menu buttons, want %d and none", locale, inline, menu, len(composerFormats))
		}
		triggers := chatPolishNodes(t, markup, func(n *xhtml.Node) bool {
			return n.Data == "button" && chatPolishAttr(n, "data-chat-disclosure-toggle") != ""
		})
		if len(triggers) != 0 || strings.Contains(markup, "Markdown") {
			t.Fatalf("%s: the toolbar still carries a menu trigger or the word Markdown", locale)
		}
	}
	// The row is the only form: no width rule swaps it for a menu.
	for _, want := range []string{`.format-inline{display:flex`} {
		if !strings.Contains(Stylesheet, want) {
			t.Errorf("stylesheet missing %q", want)
		}
	}
	for _, gone := range []string{`.format-tools-menu{display:none`, `@media(max-width:479px){.format-inline,.format-tools .format-kind{display:none}`} {
		if strings.Contains(Stylesheet, gone) {
			t.Errorf("stylesheet still swaps the row for a menu: %q", gone)
		}
	}
	if strings.Contains(Stylesheet, `.format-tools{display:flex}.format-button[data-extra=code]`) {
		t.Error("a width rule still hides the code, list and quote tools in the row")
	}
}

// TestTodo_CHATBUG_003_Browser renders the whole composer and checks the
// neighbouring controls: voice and location are icon buttons of the tool size
// in the same row when available and absent when not, and the three writing
// styles follow their service.
func TestTodo_CHATBUG_003_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, direct := range []bool{false, true} {
			m := chat4Fixture(locale, "sent", direct)
			m.ChatFeatures = &ChatFeatures{Locations: true, WritingStyles: true}
			markup := chatPolishMarkup(t, composer(m, handlers{}), 1440, "light")
			tools := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "composer-tools") })
			if len(tools) != 1 {
				t.Fatalf("%s: %d tool rows", locale, len(tools))
			}
			inRow := func(match func(*xhtml.Node) bool) []*xhtml.Node {
				var found []*xhtml.Node
				for _, n := range chatPolishNodes(t, markup, match) {
					if chatbugHasAncestorClass(n, "composer-tools") {
						found = append(found, n)
					}
				}
				return found
			}
			voice := inRow(func(n *xhtml.Node) bool {
				return n.Data == "button" && chatPolishAttr(n, "data-chatvoice-action") == "toggle"
			})
			if direct && len(voice) != 1 || !direct && len(voice) != 0 {
				t.Fatalf("%s direct=%v: %d voice buttons", locale, direct, len(voice))
			}
			for _, b := range voice {
				if !chatPolishHasClass(b, "tool-button") {
					t.Fatalf("%s: voice button is not a tool icon button", locale)
				}
			}
			place := inRow(func(n *xhtml.Node) bool {
				return n.Data == "button" && chatPolishAttr(n, "data-chatmap-action") == "toggle"
			})
			if len(place) != 1 || !chatPolishHasClass(place[0], "tool-button") {
				t.Fatalf("%s: location control missing or not a tool icon button (%d)", locale, len(place))
			}
			styles := inRow(func(n *xhtml.Node) bool {
				return n.Data == "button" && chatPolishAttr(n, "data-chattone-action") == "rewrite"
			})
			if len(styles) != 3 {
				t.Fatalf("%s: %d writing styles with the service available, want 3", locale, len(styles))
			}
			for _, b := range append(append(styles, place...), voice...) {
				if chatbugHasAncestorClass(b, "format-tools") {
					t.Fatalf("%s: a control sits inside the formatting tools", locale)
				}
			}
			// CHATUX-004: the formatting row sits above the tool row, in its own
			// "composer-format-row", not inside it.
			rows := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "format-inline") })
			if len(rows) != 1 || !chatbugHasAncestorClass(rows[0], "composer-format-row") || chatbugHasAncestorClass(rows[0], "composer-tools") {
				t.Fatalf("%s: formatting row missing from the composer or inside the tool row", locale)
			}

			m.ChatFeatures = &ChatFeatures{}
			markup = chatPolishMarkup(t, composer(m, handlers{}), 1440, "light")
			for _, absent := range []string{`data-chatmap-action="toggle"`, `data-chattone-action="rewrite"`} {
				if strings.Contains(markup, absent) {
					t.Fatalf("%s: %s rendered although its service is unavailable", locale, absent)
				}
			}
		}
	}
}

func chatbugMenuMarkup(t *testing.T, m Model, msg Message) (string, []*xhtml.Node) {
	t.Helper()
	markup := chatPolishMarkup(t, spanOf(chatMessageMenuItems(m, msg)), 1440, "light")
	entries := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "role") == "menuitem" })
	return markup, entries
}

func chatbugMenuCounts(t *testing.T, m Model, msg Message) (del, report, remove, separators int, entries []*xhtml.Node) {
	t.Helper()
	markup, entries := chatbugMenuMarkup(t, m, msg)
	for _, n := range entries {
		switch open := chatPolishAttr(n, "data-chatremove-open"); {
		case chatPolishAttr(n, "data-action") == "delete":
			del++
		case strings.Contains(open, "action=report"):
			report++
		case strings.Contains(open, "action=remove"):
			remove++
		}
	}
	return del, report, remove, strings.Count(markup, `role="separator"`), entries
}

// TestTodo_CHATBUG_008 pins who is offered what in the message menu and the
// wording and colour of the saved entry.
func TestTodo_CHATBUG_008(t *testing.T) {
	m := chat4Fixture("en-US", "sent", false)
	own := Message{ID: "own", AuthorID: m.CurrentUser, Body: "mine"}
	other := Message{ID: "other", AuthorID: "bob", Author: "Bob", Body: "theirs"}

	del, report, remove, _, _ := chatbugMenuCounts(t, m, own)
	if del != 1 || report != 0 || remove != 0 {
		t.Fatalf("own message, no permission: delete=%d report=%d remove=%d", del, report, remove)
	}
	del, report, remove, _, _ = chatbugMenuCounts(t, m, other)
	if del != 0 || report != 1 || remove != 0 {
		t.Fatalf("someone else's message, no permission: delete=%d report=%d remove=%d", del, report, remove)
	}

	m.IsTenantAdmin = true
	// CHATBUG-030 changed this line: on a person's own message the removal
	// permission adds nothing; Delete message is the one destructive command.
	del, report, remove, separators, entries := chatbugMenuCounts(t, m, own)
	if del != 1 || report != 0 || remove != 0 || separators != 1 {
		t.Fatalf("own message, with removal permission: delete=%d report=%d remove=%d separators=%d", del, report, remove, separators)
	}
	if last := entries[len(entries)-1]; chatPolishAttr(last, "data-action") != "delete" {
		t.Fatal("Delete message is not the last entry of the own-message menu")
	}
	del, report, remove, separators, entries = chatbugMenuCounts(t, m, other)
	if separators != 2 {
		t.Fatalf("someone else's message, with removal permission: %d separators, want 2", separators)
	}
	if last := entries[len(entries)-1]; !strings.Contains(chatPolishAttr(last, "data-chatremove-open"), "action=remove") {
		t.Fatal("Remove for everyone is not the last entry, after its own separator")
	}
	if del != 0 || report != 1 || remove != 1 {
		t.Fatalf("someone else's message, with removal permission: delete=%d report=%d remove=%d", del, report, remove)
	}
	m.IsTenantAdmin = false
	m.Conversations[0].OwnerID = m.CurrentUser
	if _, _, remove, _, _ = chatbugMenuCounts(t, m, other); remove != 1 {
		t.Fatal("the channel owner is not offered Remove message")
	}
}

// TestTodo_CHATBUG_008_Browser renders the menu in the three locales and
// checks the saved entry, its hover-bar twin and the styles.
func TestTodo_CHATBUG_008_Browser(t *testing.T) {
	want := map[string]string{"en-US": "Remove from Saved", "de-DE": "Aus Gespeichert entfernen", "ar": "إزالة من المحفوظات"}
	for locale, label := range want {
		m := chat4Fixture(locale, "sent", false)
		msg := Message{ID: "other", AuthorID: "bob", Author: "Bob", Body: "theirs"}
		_, entries := chatbugMenuMarkup(t, m, msg)
		for _, n := range append(entries, chatPolishNodes(t, renderNode(t, chatsaveAction(m, msg, false)), func(n *xhtml.Node) bool { return n.Data == "button" })...) {
			if chatPolishAttr(n, "data-saved-action") != "save" {
				continue
			}
			if got := chatPolishAttr(n, "data-saved-remove"); got != label {
				t.Fatalf("%s: saved-state label %q, want %q", locale, got, label)
			}
			if chatPolishHasClass(n, "danger") {
				t.Fatalf("%s: the saved entry is styled as destructive", locale)
			}
		}
		if SavedMessagesCopy(locale).Unsave != label {
			t.Fatalf("%s: copy %q, want %q", locale, SavedMessagesCopy(locale).Unsave, label)
		}
		red := 0
		for _, n := range entries {
			if chatPolishHasClass(n, "danger") {
				red++
			}
		}
		if red != 1 {
			t.Fatalf("%s: %d destructive entries on someone else's message, want only Report", locale, red)
		}
	}
	if !strings.Contains(ChatsaveStyles, `.menu-item.chatsave-action[aria-pressed="true"]{color:var(--hcm-color-text)}`) {
		t.Error("a saved menu entry still takes the brand red")
	}
	// The menu opens clear of the bar that opened it.
	bar := chatLayerRect{900, 520, 1000, 548}
	g := anchoredChatGeometry(bar, 360, 320, 1440, 900, true, false)
	if g.top+g.height > bar.top-8 {
		t.Fatalf("menu touches the hover bar: %+v", g)
	}
}
