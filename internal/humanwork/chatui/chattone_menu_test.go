package chatui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
)

// TestChattoneRewriteMenu pins the composer's writing-style control: one button
// that says "Rewrite" in words and a menu of three rows, each with the style's
// name, one line of description and its shortcut, in English, German and Arabic.
// The three pencil icons that stood here before said nothing about what they were.
func TestChattoneRewriteMenu(t *testing.T) {
	registers := []string{"professional", "friendly", "concise"}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, target := range []string{"chat-composer", "thread-composer"} {
			p := ChattoneToolbarProps{Locale: locale, Target: target, Conversation: "room", Draft: "Please retain this draft", Styles: chatrewrite.DefaultStyles(), Enabled: true,
				Suggestion: &chatrewrite.Suggestion{StyleID: "concise", ReasonKey: "short"}}
			markup := chattoneMarkup(t, p)
			where := locale + " " + target

			// One labelled button, in the tool row's own shape, with a distinct icon.
			triggers := chatPolishNodes(t, markup, func(n *xhtml.Node) bool {
				return n.Data == "button" && chatPolishAttr(n, "data-chattone-action") == "toggle"
			})
			if len(triggers) != 1 {
				t.Fatalf("%s: %d Rewrite buttons, want 1", where, len(triggers))
			}
			trigger, word := triggers[0], ChattoneText(locale, "rewrite")
			if word == "" || strings.HasPrefix(word, "rewrite") {
				t.Fatalf("%s: the Rewrite word is missing from the copy table: %q", where, word)
			}
			if !chatPolishHasClass(trigger, "tool-button") || !chatPolishAncestor(trigger, "chattone-toolbar-choices") {
				t.Fatalf("%s: the Rewrite button is not a tool-row button", where)
			}
			if chatPolishAttr(trigger, "aria-label") != word || !strings.Contains(chat5NodeText(trigger), word) || chatPolishAttr(trigger, "title") == "" {
				t.Fatalf("%s: the Rewrite button has no name, visible word or tooltip: %s", where, markup)
			}
			if chatPolishAttr(trigger, "aria-haspopup") != "menu" || chatPolishAttr(trigger, "aria-expanded") != "false" || chatPolishAttr(trigger, "aria-controls") != target+"-chattone-menu" {
				t.Fatalf("%s: the Rewrite button does not describe the menu it opens", where)
			}
			if !strings.Contains(markup, `icon-sparkles`) || strings.Contains(markup, `icon-edit`) {
				t.Fatalf("%s: the Rewrite icon is not its own glyph", where)
			}
			if strings.Contains(markup, `role="tooltip"`) || strings.Contains(markup, `chattone-style"`) {
				t.Fatalf("%s: the old per-style pencil buttons are still drawn", where)
			}

			// A menu in the shared anchored layer, with the three rows.
			menus := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "id") == target+"-chattone-menu" })
			if len(menus) != 1 || chatPolishAttr(menus[0], "data-chat-layer") != "writing-style" || chatPolishAttr(menus[0], "popover") != "manual" || !hasChatPolishAttribute(menus[0], "hidden") {
				t.Fatalf("%s: the menu is not a hidden anchored layer: %s", where, markup)
			}
			lists := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "role") == "menu" })
			if len(lists) != 1 || chatPolishAttr(lists[0], "aria-label") != ChattoneText(locale, "menu") {
				t.Fatalf("%s: the menu list is missing or unnamed", where)
			}
			rows := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "role") == "menuitem" })
			if len(rows) != 3 {
				t.Fatalf("%s: %d menu rows, want 3", where, len(rows))
			}
			for i, row := range rows {
				register := registers[i]
				if chatPolishAttr(row, "data-chattone-action") != "rewrite" || chatPolishAttr(row, "data-style") != register || chatPolishAttr(row, "data-index") != itoa(i+1) || !chatPolishHasClass(row, "menu-item") {
					t.Fatalf("%s row %d: the click handler's hooks are missing: %s", where, i+1, markup)
				}
				text := chat5NodeText(row)
				for _, want := range []string{ChattoneText(locale, register), ChattoneText(locale, register+"_help"), "Alt+Shift+" + itoa(i+1)} {
					if want == "" || !strings.Contains(text, want) {
						t.Fatalf("%s row %d: %q is missing from %q", where, i+1, want, text)
					}
				}
				if chatPolishAttr(row, "aria-label") != ChattoneText(locale, register) || chatPolishAttr(row, "aria-keyshortcuts") != "Alt+Shift+"+itoa(i+1) {
					t.Fatalf("%s row %d: no accessible name or shortcut", where, i+1)
				}
				// The suggested tag, with its reason, is on the suggested row only.
				tags := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "chattone-suggested") })
				if len(tags) != 1 {
					t.Fatalf("%s: %d suggested tags, want exactly 1", where, len(tags))
				}
				suggested := strings.Contains(text, ChattoneText(locale, "suggested"))
				if suggested != (register == "concise") {
					t.Fatalf("%s row %d: suggested tag is on the wrong row", where, i+1)
				}
				if suggested && !strings.Contains(text, ChattoneText(locale, "short")) {
					t.Fatalf("%s: the suggested row does not say why", where)
				}
			}

			// The sentence about what is sent, read before a style is chosen.
			notices := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "chattone-menu-notice") })
			if len(notices) != 1 || !chatPolishAncestor(notices[0], "chattone-menu") || strings.TrimSpace(chat5NodeText(notices[0])) != ChattoneText(locale, "notice") {
				t.Fatalf("%s: the menu does not carry the notice", where)
			}
			// Nothing else floats: the old result popover (notice, status, Undo) is gone
			// from the toolbar; what a rewrite leaves is drawn above the text.
			if strings.Contains(markup, "chattone-options") || strings.Contains(markup, "chattone-preview") || strings.Contains(markup, "chattone-status") {
				t.Fatalf("%s: the toolbar still carries the result popover: %s", where, markup)
			}
			if locale == "ar" && !strings.Contains(markup, `dir="rtl"`) {
				t.Fatalf("%s: not right-to-left", where)
			}

			// No suggestion: no tag anywhere.
			p.Suggestion = nil
			if strings.Contains(chattoneMarkup(t, p), "chattone-suggested") {
				t.Fatalf("%s: a suggested tag without a suggestion", where)
			}
		}
	}
}

// TestChattoneRewriteMenuStates: the control is out of the way below three
// words, switched off as one while the composer or the service is, and absent
// when the feature is.
func TestChattoneRewriteMenuStates(t *testing.T) {
	base := ChattoneToolbarProps{Locale: "en-US", Target: "chat-composer", Conversation: "room", Draft: "one two three", Styles: chatrewrite.DefaultStyles(), Enabled: true}

	if got := chattoneMarkup(t, base); !strings.Contains(got, `data-ready="true"`) {
		t.Fatalf("three words are not enough: %s", got)
	}
	short := base
	short.Draft = "two words"
	if got := chattoneMarkup(t, short); !strings.Contains(got, `data-ready="false"`) {
		t.Fatalf("two words show the control: %s", got)
	}
	// data-ready is what hides it: the rule is in the stylesheet.
	if !strings.Contains(ChattoneStyles, `.chattone[data-ready="false"]`) {
		t.Fatal("nothing hides the control below three words")
	}

	disabled := base
	disabled.Disabled = true
	markup := chattoneMarkup(t, disabled)
	for _, n := range chatPolishNodes(t, markup, func(n *xhtml.Node) bool {
		return chatPolishAttr(n, "data-chattone-action") != "" && n.Data == "button" && !chatPolishAncestor(n, "chattone-options")
	}) {
		if !hasChatPolishAttribute(n, "disabled") {
			t.Fatalf("a Rewrite control is live while the composer is disabled: %s", chatPolishAttr(n, "data-chattone-action"))
		}
	}
	if !strings.Contains(markup, `data-disabled="true"`) {
		t.Fatalf("the toolbar does not say it is disabled: %s", markup)
	}

	pending := base
	pending.Pending = true
	markup = chattoneMarkup(t, pending)
	roots := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "data-chattone") == "toolbar" })
	if len(roots) != 1 || !hasChatPolishAttribute(roots[0], "hidden") || strings.Contains(markup, `data-available="true"`) {
		t.Fatalf("the control shows before the server has answered: %s", markup)
	}

	off := base
	off.Enabled = false
	if got := chattoneMarkup(t, off); got != "" {
		t.Fatalf("the control renders with the feature off: %s", got)
	}

	// The busy state swaps the word for the working label by CSS alone.
	for _, want := range []string{`.chattone[aria-busy="true"] .chattone-trigger-label{display:none}`, `.chattone[aria-busy="true"] .chattone-trigger-busy{display:inline}`} {
		if !strings.Contains(ChattoneStyles, want) {
			t.Fatalf("busy state rule missing: %s", want)
		}
	}
	if !strings.Contains(chattoneMarkup(t, base), ChattoneText("en-US", "rewriting")) {
		t.Fatal("the working label is not in the button")
	}
}

// TestChattoneRewriteMenuCopy: every key the control draws exists in each locale
// and the three locales differ, so none falls back to English unnoticed.
func TestChattoneRewriteMenuCopy(t *testing.T) {
	for _, key := range []string{"rewrite", "rewrite_tip", "rewriting", "menu"} {
		en, de, ar := ChattoneText("en-US", key), ChattoneText("de-DE", key), ChattoneText("ar", key)
		if en == "" || de == "" || ar == "" || en == de || en == ar || de == ar {
			t.Fatalf("%s: en %q de %q ar %q", key, en, de, ar)
		}
	}
}

// TestChattoneRewrittenLine pins what a rewrite leaves above the composer's text:
// "Rewritten: <style>" with Undo and See changes as text buttons, "(edited)" once
// the field differs from what the model wrote, and a layer for the changes that
// is drawn closed. The privacy sentence is not repeated here; it is in the menu.
func TestChattoneRewrittenLine(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		p := ChattonePreviewProps{Locale: locale, Target: "chat-composer", StyleName: "Professional", Original: "as typed <b>", Rewritten: "as written"}
		markup, err := ui.RenderToString(RenderChattonePreview(p))
		if err != nil {
			t.Fatal(err)
		}
		lead := ChattoneText(locale, "rewritten")
		if lead == "" || strings.HasPrefix(lead, "rewritten") {
			t.Fatalf("%s: no copy for the line", locale)
		}
		for _, want := range []string{lead, "Professional", `data-chattone-action="undo"`, `data-chattone-action="changes"`, `aria-expanded="false"`, `aria-haspopup="dialog"`,
			`aria-controls="chat-composer-chattone-options"`, `data-chat-layer="writing-style"`, `data-chattone-diff="true"`, `data-chattone-for="chat-composer"`,
			ChattoneText(locale, "undo_chip"), ChattoneText(locale, "changes"), ChattoneText(locale, "before"), ChattoneText(locale, "after"), "as typed &lt;b&gt;"} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s: %q is missing from %s", locale, want, markup)
			}
		}
		if strings.Contains(markup, "chattone-chip-edited") || strings.Contains(markup, ChattoneText(locale, "notice")) {
			t.Fatalf("%s: an unedited rewrite says edited, or repeats the notice: %s", locale, markup)
		}
		diff := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "chattone-options") })
		if len(diff) != 1 || !hasChatPolishAttribute(diff[0], "hidden") {
			t.Fatalf("%s: the changes layer is not drawn closed", locale)
		}
		if locale == "ar" && !strings.Contains(markup, `dir="rtl"`) {
			t.Fatal("ar: not right-to-left")
		}
		p.Edited = true
		edited, _ := ui.RenderToString(RenderChattonePreview(p))
		if !strings.Contains(edited, ChattoneText(locale, "edited_tag")) || !strings.Contains(edited, `data-chattone-action="undo"`) {
			t.Fatalf("%s: an edited rewrite lost its mark or its Undo: %s", locale, edited)
		}
		if got := ChattoneText(locale, "edited_tag"); got == "" || (locale != "en-US" && got == ChattoneText("en-US", "edited_tag")) {
			t.Fatalf("%s: edited tag %q", locale, got)
		}
	}
	if node := RenderChattonePreview(ChattonePreviewProps{Locale: "en-US", Target: "chat-composer"}); node != nil {
		t.Fatal("a line is drawn with nothing rewritten")
	}
}

// TestChattoneChipSlot: the slot is in both composers, above the text, empty
// until the browser fills it, and absent where the feature is off.
func TestChattoneChipSlot(t *testing.T) {
	m := chat4Fixture("en-US", "sent", false)
	m.ChatFeatures = &ChatFeatures{WritingStyles: true}
	main := renderNode(t, composer(m, handlers{}))
	slot, field := strings.Index(main, `id="chat-composer-chattone-chip"`), strings.Index(main, `id="chat-composer"`)
	if slot < 0 || field < 0 || slot > field {
		t.Fatalf("the slot is not above the main composer's text (%d, %d)", slot, field)
	}
	if !strings.Contains(main, `class="chattone-chip-region"></div>`) || !strings.Contains(main, `role="status"`) {
		t.Fatal("the slot is not an empty region with a status line")
	}
	m.ShowThread, m.ThreadParentID = true, "question"
	m.ThreadMessages = []Message{{ID: "reply", Author: "Alex", Body: "Reply"}}
	thread := renderNode(t, threadPane(m, handlers{}))
	slot, field = strings.Index(thread, `id="thread-composer-chattone-chip"`), strings.Index(thread, `id="thread-composer"`)
	if slot < 0 || field < 0 || slot > field {
		t.Fatalf("the slot is not above the thread composer's text (%d, %d)", slot, field)
	}
	m.ChatFeatures = &ChatFeatures{}
	if strings.Contains(renderNode(t, composer(m, handlers{})), "chattone-chip") {
		t.Fatal("the slot is drawn with the feature off")
	}
}
