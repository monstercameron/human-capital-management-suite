package chatui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	xhtml "golang.org/x/net/html"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const chatux003Body = "Carry over up to 40 hours.\n\nSources\n" +
	"- [Paid time off policy · v1.0.0](/workspace/app/docs?document=pto&version=v1) <!--chat.agent.source.readable:true-->\n" +
	"- Executive succession plan <!--chat.agent.source.readable:false-->"

// chatux003Card renders the answer card for one fixture state.
func chatux003Card(t *testing.T, locale string, width int, reason string, configure func(*Model)) (string, *xhtml.Node) {
	t.Helper()
	model := chat4Fixture(locale, "answered", false)
	body := chatux003Body
	if reason != "" {
		body += "\n<!--chat.agent.private:" + reason + "-->"
	}
	model.EphemeralMessages[0].Body = body
	model.Callbacks.ShareAgentAnswer = func(string) {}
	model.Callbacks.OpenMenu = func(string) {}
	model.PersonaPostActors = map[string]PersonaPostActor{"answer": {Display: "Policy Helper", Actor: PersonaActor{PersonaID: "policy-helper", AgentID: "policy-helper", Trusted: true}}}
	if configure != nil {
		configure(&model)
	}
	rows := personaReplyRowsForPost(model, localUI{}, "question", time.Now())
	if len(rows) != 1 {
		t.Fatalf("%s/%d: %d answer rows", locale, width, len(rows))
	}
	markup := chatPolishMarkup(t, rows[0], width, "light")
	cards := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "article" && chatPolishHasClass(n, "chat-ephemeral") })
	if len(cards) != 1 {
		t.Fatalf("%s/%d: %d answer cards", locale, width, len(cards))
	}
	return markup, cards[0]
}

func chatux003Find(card *xhtml.Node, class string) []*xhtml.Node {
	var out []*xhtml.Node
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode && chatPolishHasClass(n, class) {
			out = append(out, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(card)
	return out
}

func chatux003Elements(n *xhtml.Node) []*xhtml.Node {
	var out []*xhtml.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == xhtml.ElementNode {
			out = append(out, c)
		}
	}
	return out
}

// TestTodo_CHATUX_003 is the answer card: one header line with who can see the
// answer at its end, the answer, why it is private, the sources as chips and one
// row of actions, in each language and at each width.
func TestTodo_CHATUX_003(t *testing.T) {
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		_, card := chatux003Card(t, locale, width, "audience", nil)
		model := chat4Fixture(locale, "answered", false)
		// The card's blocks, top to bottom: header, announcement, answer, reason, sources, actions.
		var order []string
		for _, child := range chatux003Elements(card) {
			switch {
			case child.Data == "header":
				order = append(order, "header")
			case chatPolishHasClass(child, "agent-reply-answer"):
				order = append(order, "answer")
			case chatPolishHasClass(child, "agent-reply-why"):
				order = append(order, "why")
			case chatPolishHasClass(child, "agent-reply-sources"):
				order = append(order, "sources")
			case chatPolishHasClass(child, "agent-reply-footer"):
				order = append(order, "actions")
			case chatPolishHasClass(child, "agent-reply-announcement"):
			default:
				t.Fatalf("unexpected block %s.%s in the card", child.Data, chatPolishAttr(child, "class"))
			}
		}
		if got := strings.Join(order, ","); got != "header,answer,why,sources,actions" {
			t.Fatalf("card blocks = %s", got)
		}
		// One header line: icon, name, badge, time, and the visibility note last.
		header := chatux003Find(card, "agent-reply-head")
		if len(header) != 1 {
			t.Fatalf("%d header lines", len(header))
		}
		parts := chatux003Elements(header[0])
		if len(parts) != 5 || !chatPolishHasClass(parts[0], "avatar") || !chatPolishHasClass(parts[1], "agent-reply-name") || !chatPolishHasClass(parts[2], "agent-badge") || parts[3].Data != "time" || !chatPolishHasClass(parts[4], "agent-reply-private") {
			t.Fatalf("header parts = %v", func() []string {
				var names []string
				for _, p := range parts {
					names = append(names, p.Data+"."+chatPolishAttr(p, "class"))
				}
				return names
			}())
		}
		if note := chatbug030Text(parts[4]); note != personaProgressText(model, "chat.agent.only_visible", "Only visible to you") {
			t.Fatalf("visibility note = %q", note)
		}
		// Why the answer is private, in the reader's language, naming the channel.
		why := chatbug030Text(chatux003Find(card, "agent-reply-why")[0])
		if want := chatux003Format(model, "chatux003.why.audience", map[string]string{"channel": "⁨#general⁩"}); why != want || !strings.Contains(why, "#general") {
			t.Fatalf("why = %q, want %q", why, want)
		}
		// Sources: one chip each. The one the reader may open is a link; the other is
		// a muted chip with a lock and its tooltip text, and never a link.
		chips := chatux003Find(card, "agent-reply-source")
		if len(chips) != 2 {
			t.Fatalf("%d source chips", len(chips))
		}
		if links := chatux003Find(chips[0], "agent-reply-source-link"); len(links) != 1 || chatPolishAttr(links[0], "href") != "/workspace/app/docs?document=pto&version=v1" {
			t.Fatalf("readable source is not a link: %+v", links)
		}
		if !chatPolishHasClass(chips[1], "agent-reply-source-locked") || len(chatux003Find(chips[1], "icon-lock")) != 1 || len(chatux003Find(chips[1], "agent-reply-source-link")) != 0 {
			t.Fatalf("unreadable source is not a muted locked chip")
		}
		if note := chatbug030Text(chatux003Find(chips[1], "agent-reply-source-unavailable")[0]); note != agentAnswerSourceUnavailable(locale) {
			t.Fatalf("locked chip text = %q", note)
		}
		if len(chatux003Find(card, "agent-reply-open")) != 0 {
			t.Fatal("the saved copy's link is on the card; it belongs to the more menu")
		}
		// One action row: Helpful, Not right, Ask a follow-up, Share to channel, and more.
		actions := chatux003Find(card, "agent-reply-actions")
		if len(actions) != 1 {
			t.Fatalf("%d action rows", len(actions))
		}
		have := map[string]bool{}
		var walkButtons func(*xhtml.Node)
		walkButtons = func(n *xhtml.Node) {
			if n.Type == xhtml.ElementNode && n.Data == "button" {
				have[chatPolishAttr(n, "data-action")+"|"+chatPolishAttr(n, "data-extra")] = true
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walkButtons(c)
			}
		}
		walkButtons(actions[0])
		for _, want := range []string{"agent-feedback|helpful", "agent-feedback|not-right", "agent-follow-up|/workspace/app/chat#channel=policy", "agent-share|", "menu|"} {
			if !have[want] {
				t.Fatalf("the action row lacks %q: %v", want, have)
			}
		}
		if share := chatux003Find(card, "agent-reply-share"); len(share) != 1 || chatPolishAttr(share[0], "data-id") != "run" || chatbug030Text(share[0]) != chatux003Text(model, "chatux003.share") {
			t.Fatalf("share control = %+v", share)
		}
	})
}

// TestTodo_CHATUX_003_Menu: the saved copy's link is an item in the card's more
// menu, a link to the conversation, and the menu is a real menu.
func TestTodo_CHATUX_003_Menu(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		_, card := chatux003Card(t, locale, 390, "", func(m *Model) { m.MenuID = "agent-card:answer" })
		menus := chatPolishNodes(t, chatPolishMarkup(t, chatux003Node(t, locale), 390, "light"), func(n *xhtml.Node) bool { return chatPolishAttr(n, "role") == "menu" })
		_ = card
		if len(menus) != 1 {
			t.Fatalf("%s: %d menus", locale, len(menus))
		}
		items := chatux003Find(menus[0], "agent-reply-open")
		if len(items) != 1 || chatPolishAttr(items[0], "role") != "menuitem" || chatPolishAttr(items[0], "href") != ChannelReferenceURL("policy") {
			t.Fatalf("%s: menu items = %+v", locale, items)
		}
		model := chat4Fixture(locale, "answered", false)
		if want := personaProgressText(model, "chat.agent.saved_conversation", "Saved in your conversation with") + " Policy Helper"; chatbug030Text(items[0]) != want {
			t.Fatalf("%s: menu item = %q, want %q", locale, chatbug030Text(items[0]), want)
		}
		trigger := chatPolishNodes(t, chatPolishMarkup(t, chatux003Node(t, locale), 390, "light"), func(n *xhtml.Node) bool { return chatPolishHasClass(n, "agent-reply-more") })
		if len(trigger) != 1 || chatPolishAttr(trigger[0], "data-id") != "agent-card:answer" || chatPolishAttr(trigger[0], "aria-haspopup") != "menu" || chatPolishAttr(trigger[0], "aria-expanded") != "true" {
			t.Fatalf("%s: more button = %+v", locale, trigger)
		}
	}
}

// chatux003Node is the row the menu tests render, with the menu open.
func chatux003Node(t *testing.T, locale string) ui.Node {
	t.Helper()
	model := chat4Fixture(locale, "answered", false)
	model.Callbacks.OpenMenu = func(string) {}
	model.MenuID = "agent-card:answer"
	model.PersonaPostActors = map[string]PersonaPostActor{"answer": {Display: "Policy Helper", Actor: PersonaActor{PersonaID: "policy-helper", AgentID: "policy-helper", Trusted: true}}}
	rows := personaReplyRowsForPost(model, localUI{}, "question", time.Now())
	return html.Div(html.Props{}, rows...)
}

// TestTodo_CHATUX_003_Accessibility: every control has a name, the state of the
// share control is announced, the locked chip can be focused, and nothing is
// hidden from a screen reader that a sighted reader sees.
func TestTodo_CHATUX_003_Accessibility(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		markup, card := chatux003Card(t, locale, 390, "asked", nil)
		if chatPolishAttr(card, "dir") != agentReplyDirection(locale) {
			t.Errorf("%s: card direction = %q", locale, chatPolishAttr(card, "dir"))
		}
		for _, button := range chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "button" }) {
			if chatPolishAttr(button, "aria-label") == "" && chatbug030Text(button) == "" {
				t.Errorf("%s: a button has no name: %s", locale, chatPolishAttr(button, "class"))
			}
		}
		if got := len(chatPolishNodes(t, markup, func(n *xhtml.Node) bool {
			return chatPolishAttr(n, "role") == "status" && chatPolishHasClass(n, "agent-reply-announcement")
		})); got != 1 {
			t.Errorf("%s: %d announcements of the answer", locale, got)
		}
		locked := chatux003Find(card, "agent-reply-source-locked")
		if len(locked) != 1 || chatPolishAttr(locked[0], "tabindex") != "0" {
			t.Errorf("%s: the locked chip cannot be focused to read why", locale)
		}
		model := chat4Fixture(locale, "answered", false)
		share := chatux003Find(card, "agent-reply-share")[0]
		if label := chatPolishAttr(share, "aria-label"); !strings.Contains(label, chatux003Text(model, "chatux003.share")) || !strings.Contains(label, "#general") {
			t.Errorf("%s: share control name = %q", locale, label)
		}
	}
	// While sharing, the control says so, is busy and cannot be pressed again.
	_, card := chatux003Card(t, "en-US", 390, "asked", func(m *Model) {
		m.AgentShare = map[string]AgentShareState{"run": {Status: AgentShareSharing}}
	})
	share := chatux003Find(card, "agent-reply-share")[0]
	if chatPolishAttr(share, "aria-busy") != "true" || chatbug030Text(share) != "Sharing…" || !hasAttr(share, "disabled") {
		t.Errorf("sharing control = %q busy=%q", chatbug030Text(share), chatPolishAttr(share, "aria-busy"))
	}
}

func hasAttr(n *xhtml.Node, key string) bool {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return true
		}
	}
	return false
}

// TestTodo_CHATUX_003_ShareStates: what the card says before, during and after
// "Share to channel", and when the agent never shares.
func TestTodo_CHATUX_003_ShareStates(t *testing.T) {
	for _, tc := range []struct {
		name     string
		reason   string
		state    AgentShareState
		callback bool
		button   bool
		note     string
	}{
		{name: "offered", reason: "asked", callback: true, button: true},
		{name: "private because of the audience, still offered", reason: "audience", callback: true, button: true},
		{name: "shared", reason: "asked", state: AgentShareState{Status: AgentShareShared}, callback: true, note: "Shared to ⁨#general⁩"},
		{name: "refused by the audience check", reason: "asked", state: AgentShareState{Status: AgentShareRefused, Reason: "audience"}, callback: true, note: "Not everyone in ⁨#general⁩ can open the sources, so this stays private."},
		{name: "refused because the agent is strict", reason: "asked", state: AgentShareState{Status: AgentShareRefused, Reason: "agent"}, callback: true, note: "Policy Helper always answers privately, so this stays private."},
		{name: "failed, can be tried again", reason: "asked", state: AgentShareState{Status: AgentShareFailed}, callback: true, button: true, note: "Could not share this answer. Try again."},
		{name: "a strict agent never offers it", reason: "agent", callback: true},
		{name: "no way to share here", reason: "asked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, card := chatux003Card(t, "en-US", 1440, tc.reason, func(m *Model) {
				if !tc.callback {
					m.Callbacks.ShareAgentAnswer = nil
				}
				if tc.state.Status != "" {
					m.AgentShare = map[string]AgentShareState{"run": tc.state}
				}
			})
			share := chatux003Find(card, "agent-reply-share")
			if tc.button != (len(share) == 1) {
				t.Fatalf("share control present = %v, want %v", len(share) == 1, tc.button)
			}
			notes := chatux003Find(card, "agent-reply-share-note")
			if tc.note == "" && len(notes) != 0 || tc.note != "" && (len(notes) != 1 || chatbug030Text(notes[0]) != tc.note) {
				t.Fatalf("share note = %v, want %q", notes, tc.note)
			}
			// An agent that never shares never says anything about sharing.
			if tc.reason == "agent" {
				if why := chatbug030Text(chatux003Find(card, "agent-reply-why")[0]); why != "Policy Helper always answers privately." {
					t.Fatalf("why = %q", why)
				}
			}
		})
	}
}

// TestTodo_CHATUX_003_NoReasonNoLine: a private answer the server gave no reason
// for (a direct conversation, an older answer) carries no reason line and
// still has its actions; the question's agent can always be asked again.
func TestTodo_CHATUX_003_NoReasonNoLine(t *testing.T) {
	_, card := chatux003Card(t, "en-US", 1440, "", func(m *Model) { m.PersonaInvocations[0].Projection.InvocationID = "" })
	if len(chatux003Find(card, "agent-reply-why")) != 0 {
		t.Fatal("a reason line appeared with no reason")
	}
	footer := chatux003Find(card, "agent-reply-footer")
	if len(footer) != 1 || len(chatux003Find(footer[0], "agent-follow-up")) != 1 || len(chatux003Find(footer[0], "agent-feedback")) != 0 || len(chatux003Find(footer[0], "agent-reply-share")) != 0 {
		t.Fatal("an answer that cannot be rated or shared lost its follow-up")
	}
}

// TestTodo_CHATUX_003_Actions: the card's clicks reach the application.
func TestTodo_CHATUX_003_Actions(t *testing.T) {
	var shared []string
	model := chat4Fixture("en-US", "answered", false)
	model.Callbacks.ShareAgentAnswer = func(id string) { shared = append(shared, id) }
	chatux003ShareClick(model, localUI{}, "run")
	chatux003ShareClick(model, localUI{}, "")
	model.AgentShare = map[string]AgentShareState{"run": {Status: AgentShareSharing}}
	chatux003ShareClick(model, localUI{}, "run")
	model.AgentShare = map[string]AgentShareState{"run": {Status: AgentShareShared}}
	chatux003ShareClick(model, localUI{}, "run")
	model.AgentShare = map[string]AgentShareState{"run": {Status: AgentShareFailed}}
	chatux003ShareClick(model, localUI{}, "run")
	if fmt.Sprint(shared) != "[run run]" {
		t.Fatalf("share requests = %v, want one for a first click and one retry after a failure", shared)
	}
	// Ask a follow-up mentions the agent in this conversation's composer; an agent
	// that cannot be mentioned here opens the person's conversation with it.
	reference, ok := chatux003FollowUpReference(model, "policy-helper")
	if !ok || reference.Display != "Policy Helper" || reference.ConversationID != "general" {
		t.Fatalf("follow-up reference = %+v %v", reference, ok)
	}
	if _, ok := chatux003FollowUpReference(model, "someone-else"); ok {
		t.Fatal("an agent that is not in this conversation was offered a mention")
	}
	var selected string
	model.Callbacks.SelectConversation = func(id string) { selected = id }
	chatux003AskFollowUp(model, mentionStore{}, "someone-else", ChannelReferenceURL("policy"))
	if selected != "policy" {
		t.Fatalf("fallback selected %q", selected)
	}
}

// TestTodo_CHATUX_003_Browser renders the cards the way a browser lays them out
// and checks the stylesheet that does it: tokens only, one line of chips that
// wraps, the tooltip is the sentence, and the working card keeps "Stop" whole
// at 390 px with the elapsed time on its own line at the end.
func TestTodo_CHATUX_003_Browser(t *testing.T) {
	chat4Require(t, ChatUX003Styles,
		`.agent-reply-head .agent-reply-private{display:inline-flex;align-items:center;gap:4px;margin-inline-start:auto`,
		`.agent-reply-sources{display:flex;align-items:center;gap:6px 8px;flex-wrap:wrap`,
		`.agent-reply-source{display:inline-flex`, `border-radius:999px`,
		`.agent-reply-source-locked:hover .agent-reply-source-unavailable,.agent-reply-source-locked:focus-visible .agent-reply-source-unavailable{opacity:1}`,
		`.agent-reply-actions{display:flex;flex-wrap:wrap`,
		// The working card (CHATUX-003 follow-up): "Stop" is never broken across two
		// lines, the sentence wraps instead, and the elapsed time is last, alone.
		`.agent-progress-cancel{flex:none;white-space:nowrap}`, `.agent-reply-state-line{flex-wrap:wrap`,
		`@media(max-width:480px){.agent-reply-state-divider,.agent-progress-cancel{order:1}.agent-reply-counter{order:2;flex:1 0 100%;margin-inline-start:0}}`)
	if strings.Contains(ChatUX003Styles, "#") || strings.Contains(ChatUX003Styles, "font-family") || strings.Contains(ChatUX003Styles, "rgb") {
		t.Fatal("the card bypasses the product's tokens")
	}
	if !strings.Contains(Stylesheet, ChatUX003Styles) {
		t.Fatal("the card's stylesheet is not part of the chat stylesheet")
	}
	// The working card at 390 px: one Stop button, one counter, in the order the
	// stylesheet expects (copy, dots, counter, divider, Stop), each its own element.
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		model := chat4Fixture(locale, "working15", false)
		rows := personaReplyRowsForPost(model, localUI{}, "question", time.Now())
		markup := chatPolishMarkup(t, html.Div(html.Props{}, rows...), 390, "light")
		stop := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "agent-progress-cancel") })
		counter := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "agent-reply-counter") })
		if len(stop) != 1 || len(chatux003Elements(stop[0])) != 0 || strings.TrimSpace(chatbug030Text(stop[0])) != agentAnswerStopLabel(locale) || len(counter) != 1 {
			t.Fatalf("%s: working card has %d Stop buttons (%q) and %d counters", locale, len(stop), chatbug030Text(stop[0]), len(counter))
		}
	}
}

// TestTodo_CHATUX_003_AskAgain: a card that ends because the answer was
// interrupted, with no run behind it to retry, offers the one useful action, Ask
// again, while the question is recent. It asks the same question again only on
// the person's click, and never for a question that is not theirs.
func TestTodo_CHATUX_003_AskAgain(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		render := func(model Model) []*xhtml.Node {
			rows := personaReplyRowsForPost(model, localUI{}, "question", time.Now())
			markup := chatPolishMarkup(t, html.Div(html.Props{}, rows...), 390, "light")
			return chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "agent-ask-again") })
		}
		interrupted := func(age time.Duration, provisional bool) Model {
			model := chat4Fixture(locale, "working15", false)
			model.Messages[0].SentAt = time.Now().Add(-age)
			model.PersonaInvocations[0].Projection.Progress.Deadline = time.Now().Add(-time.Second)
			model.PersonaInvocations[0].Projection.Progress.Provisional = provisional
			return model
		}
		fresh := render(interrupted(time.Minute, true))
		if len(fresh) != 1 || chatPolishAttr(fresh[0], "data-action") != "agent-ask-again" || chatPolishAttr(fresh[0], "data-id") != "question" || chatbug030Text(fresh[0]) != chatux003Text(chat4Fixture(locale, "sent", false), "chatux003.ask_again") {
			t.Fatalf("%s: an interrupted card with no run offers %+v", locale, fresh)
		}
		if got := render(interrupted(9*time.Hour, true)); len(got) != 0 {
			t.Fatalf("%s: Ask again offered nine hours after the question", locale)
		}
		// A failure the server reported that cannot be retried offers it too.
		failed := chat4Fixture(locale, "failed", false)
		failed.PersonaInvocations[0].Projection.Failure = &PersonaProgressFailure{InvocationID: "run", InvokerID: "alice", Code: "ANSWER_INTERRUPTED"}
		if got := render(failed); len(got) != 1 {
			t.Fatalf("%s: an interrupted failure offers %d Ask again buttons", locale, len(got))
		}
	}
	var sent []string
	model := chat4Fixture("en-US", "sent", false)
	model.Callbacks.SendMessageWithReferences = func(conversation, body string, refs []ChatReference) {
		sent = append(sent, conversation+"|"+body+"|"+refs[0].ID)
	}
	chatux003AskAgain(model, "question")
	chatux003AskAgain(model, "no-such-post")
	other := model
	other.CurrentUser = "bob"
	chatux003AskAgain(other, "question")
	if len(sent) != 1 || sent[0] != "general|@Policy Helper explain our PTO policy in detail: accrual|policy-helper" {
		t.Fatalf("asked again = %v, want the same question once, from its own author only", sent)
	}
}
