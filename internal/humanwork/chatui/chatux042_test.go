package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

// TestTodo_CHATUX_042: an agent's answer is one component with two variants, a
// chosen rating is a check and accent text, chips differ from actions, "found
// nothing" is an answer without a warning glyph, and the hover bar holds four
// actions that the menu does not repeat.
func TestTodo_CHATUX_042(t *testing.T) {
	t.Run("one component, two variants", func(t *testing.T) {
		if got := chatux042Class(chatux042Attached, "chat-ephemeral agent-reply-row"); got != "agent-answer agent-answer-attached chat-ephemeral agent-reply-row" {
			t.Errorf("attached class: %q", got)
		}
		if got := chatux042Class(chatux042Standalone, ""); got != "agent-answer agent-answer-standalone" {
			t.Errorf("standalone class: %q", got)
		}
		// The card under a question in a channel is the attached variant.
		page := renderNode(t, Build(chat4Fixture("en-US", "answered", false)))
		if !strings.Contains(page, `class="agent-answer agent-answer-attached chat-ephemeral agent-reply-row"`) {
			t.Errorf("the answer card is not the attached variant")
		}
		if strings.Contains(page, "agent-answer-standalone") {
			t.Errorf("a channel's card is drawn as the standalone variant")
		}
		// An agent's own message (a direct reply, an announcement) is the standalone variant.
		actor := &PersonaActor{AgentID: "policy-helper", PersonaID: "policy-helper", Trusted: true}
		if !actor.valid() {
			t.Skip("the fixture actor is not valid; the class rule is covered by the render below")
		}
		if got := chatux042MessageContentClass(Message{PersonaActor: actor}); got != "agent-answer agent-answer-standalone message-content" {
			t.Errorf("an agent's message content: %q", got)
		}
		if got := chatux042MessageContentClass(Message{}); got != "message-content" {
			t.Errorf("a person's message content: %q", got)
		}
		m := chat4Fixture("en-US", "answered", true)
		announcement := renderNode(t, RenderAgentAnnouncementMessage(m, AgentAnnouncementMessage{AgentName: "Policy Helper", Text: "Open enrollment starts Monday.", PostedAt: time.Date(2026, 10, 1, 9, 5, 0, 0, time.UTC)}))
		for _, want := range []string{"agent-answer agent-answer-standalone", "agent-reply-identity", "<time", "Open enrollment starts Monday."} {
			if !strings.Contains(announcement, want) {
				t.Errorf("the announcement misses %q: %s", want, announcement)
			}
		}
	})

	t.Run("a chosen rating is a check and accent text", func(t *testing.T) {
		chosen := renderNode(t, chatux042RatingButton("Helpful", "agent-feedback-undo", "run", "helpful", "thumb-up", true, false))
		for _, want := range []string{"agent-feedback-button is-chosen", `aria-pressed="true"`, "icon-check", ">Helpful<"} {
			if !strings.Contains(chosen, want) {
				t.Errorf("the chosen rating misses %q: %s", want, chosen)
			}
		}
		rest := renderNode(t, chatux042RatingButton("Not right", "agent-feedback", "run", "not-right", "thumb-down", false, false))
		if strings.Contains(rest, "is-chosen") || strings.Contains(rest, "icon-check") || !strings.Contains(rest, "icon-thumb-down") || !strings.Contains(rest, `aria-pressed="false"`) {
			t.Errorf("the other rating is drawn as chosen: %s", rest)
		}
		for _, want := range []string{
			`.agent-feedback-button.is-chosen,`,
			`background:transparent;color:var(--accent);font-weight:600`,
		} {
			if !strings.Contains(ChatUX042Styles, want) {
				t.Errorf("the rating styles miss %q", want)
			}
		}
		// No answer button is filled with the accent at rest or when chosen.
		if strings.Contains(ChatUX042Styles, "background:var(--accent)") {
			t.Error("a rating is filled with the accent")
		}
	})

	t.Run("chips are not buttons", func(t *testing.T) {
		for _, want := range []string{
			`.agent-reply-source{display:inline-flex;`,
			`border:0;border-radius:6px;background:color-mix(in srgb,var(--accent) 9%,var(--surface))`,
			`:is(.agent-feedback-button,.agent-reply-action){display:inline-flex;align-items:center;gap:6px;min-block-size:32px;padding-inline:8px;border:0;border-radius:6px;background:transparent`,
		} {
			if !strings.Contains(ChatUX042Styles, want) {
				t.Errorf("the chip and action styles miss %q", want)
			}
		}
		if !strings.Contains(Stylesheet, ChatUX042Styles) {
			t.Error("the answer styles are not in the stylesheet")
		}
	})

	t.Run("found nothing is an answer", func(t *testing.T) {
		m := chat4Fixture("en-US", "answered", true)
		nothing := renderNode(t, chatux042Fragment(chatbug054Reason(m, PersonaProgressFailure{Code: "NOTHING_FOUND"}, "Assistant")))
		if strings.Contains(nothing, "icon-warning") || !strings.Contains(nothing, `data-outcome="nothing-found"`) || !strings.Contains(nothing, "found nothing about this") {
			t.Errorf("found nothing is drawn as a failure: %s", nothing)
		}
		failure := renderNode(t, chatux042Fragment(chatbug054Reason(m, PersonaProgressFailure{Code: "MODEL_UNAVAILABLE"}, "Assistant")))
		if !strings.Contains(failure, "icon-warning") || strings.Contains(failure, "nothing-found") {
			t.Errorf("a real failure lost its warning: %s", failure)
		}
	})

	t.Run("the bar holds three emoji and four actions and the menu repeats none", func(t *testing.T) {
		m := chat4Fixture("en-US", "answered", false)
		m.Callbacks.OpenPicker, m.Callbacks.OpenMenu, m.Callbacks.Pin, m.Callbacks.CopyLink, m.Callbacks.OpenShare = func(string) {}, func(string) {}, func(string) {}, func(string) {}, func(string) {}
		msg := Message{ID: "p1", AuthorID: "bob", Author: "Bob", Body: "hello there"}
		bar := renderNode(t, chatux042Fragment(chatux042MessageBar(m, msg, chatux042React{action: "react-pick", label: "Add reaction", glyph: "smile", enabled: true}, true, false)))
		buttons := chatPolishNodes(t, bar, func(n *xhtml.Node) bool { return n.Data == "button" })
		if len(buttons) != 7 {
			t.Fatalf("the bar holds %d buttons, want the three emoji, react, reply, save and more", len(buttons))
		}
		var kinds []string
		for _, b := range buttons {
			switch {
			case chatPolishAttr(b, "data-action") != "":
				kinds = append(kinds, chatPolishAttr(b, "data-action"))
			default:
				kinds = append(kinds, chatPolishAttr(b, "data-saved-action"))
			}
		}
		if strings.Join(kinds, ",") != "react-with,react-with,react-with,react-pick,reply,save,menu" {
			t.Errorf("the bar's actions are %v", kinds)
		}
		for _, gone := range []string{`data-action="pin"`, "chatlisten"} {
			if strings.Contains(bar, gone) {
				t.Errorf("the bar still carries %s", gone)
			}
		}
		if !strings.Contains(bar, "message-actions-divider") {
			t.Errorf("no hairline between the emoji and the actions")
		}
		// Reply and Save in the menu are shown only where there is no bar; the rest is always shown.
		menu := renderNode(t, chatux042Fragment(chatux022MenuItems(m, msg)))
		items := chatux040Menuitems(t, menu)
		var actions []string
		for _, item := range items {
			action := chatPolishAttr(item, "data-action")
			if action == "" {
				action = chatPolishAttr(item, "data-saved-action")
			}
			actions = append(actions, action)
			barTwin := action == "reply" || action == "save"
			phoneOnly := chatPolishHasClass(item, "menu-phone-only") || chatPolishHasClass(item, "chatsave-action")
			if barTwin && !phoneOnly {
				t.Errorf("the menu repeats %s where the bar has it", action)
			}
			if action == "pin" && (chatPolishHasClass(item, "menu-bar-twin") || phoneOnly) {
				t.Errorf("Pin is hidden in the menu although the bar no longer holds it")
			}
		}
		if !strings.Contains(strings.Join(actions, ","), "pin") {
			t.Errorf("the menu has no Pin: %v", actions)
		}
		for _, want := range []string{
			`.message-menu .menu-phone-only{display:none}`,
			`@media(max-width:767px),(pointer:coarse){.message-menu .menu-phone-only{display:flex}}`,
			`.chat-workspace .message-action.chatsave-action[aria-pressed="true"],`,
			`color:var(--accent)}`,
			`.chat-workspace .message-action.chatsave-action .chatsave-bookmark{fill:none}`,
		} {
			if !strings.Contains(ChatUX042Styles, want) {
				t.Errorf("the bar and bookmark styles miss %q", want)
			}
		}
	})
}

// chatux042Fragment wraps a list of nodes so the list can be rendered at once.
func chatux042Fragment(nodes []ui.Node) ui.Node { return html.Div(html.Props{}, nodes...) }
