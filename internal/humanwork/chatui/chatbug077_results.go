package chatui

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

// CHATBUG-077 and CHATBUG-055. A search result is drawn the way the message it
// is would be drawn in the conversation: the avatar and name of its author, the
// time, and the text through the conversation's own body renderer (mentions as
// chips, documents by their titles), under one small line that says which
// conversation it is in. The tint over the searched words is one mark per
// phrase. A message that more than one source returns is one result.

// chatsearchMessageKey names the message behind a result, or "" when the result
// is not one message (a person, a channel, a file, a to-do).
func chatsearchMessageKey(row chatsearch.Row) string {
	switch row.Kind {
	case chatsearch.Message, chatsearch.Thread, chatsearch.Pin, chatsearch.AgentAnswer, chatsearch.Saved, chatsearch.Voice:
		if row.Target.MessageID != "" {
			return row.Target.ConversationID + "\x00" + row.Target.MessageID
		}
	}
	return ""
}

// chatsearchDedupe lists each message once. An agent's answer is the result for
// the message that holds it, so the same post is not listed again under
// Messages; a saved message is the message's own result with a bookmark, and
// the second copy is dropped. A group left with nothing is dropped too.
// saved holds the keys of the messages the person has saved.
func chatsearchDedupe(groups []chatsearch.Group) ([]chatsearch.Group, map[string]bool) {
	answers := map[string]bool{}
	for _, g := range groups {
		for _, row := range g.Rows {
			if row.Kind == chatsearch.AgentAnswer {
				if key := chatsearchMessageKey(row); key != "" {
					answers[key] = true
				}
			}
		}
	}
	listed := map[string]bool{}
	out := make([]chatsearch.Group, 0, len(groups))
	for _, g := range groups {
		if g.Kind == chatsearch.Saved {
			out = append(out, g)
			continue
		}
		kept := make([]chatsearch.Row, 0, len(g.Rows))
		for _, row := range g.Rows {
			key := chatsearchMessageKey(row)
			if key != "" && answers[key] && (row.Kind == chatsearch.Message || row.Kind == chatsearch.Thread) {
				continue
			}
			if key != "" && (row.Kind == chatsearch.Message || row.Kind == chatsearch.Thread || row.Kind == chatsearch.AgentAnswer) {
				listed[key] = true
			}
			kept = append(kept, row)
		}
		if len(kept) > 0 {
			g.Rows = kept
			out = append(out, g)
		}
	}
	saved := map[string]bool{}
	result := make([]chatsearch.Group, 0, len(out))
	for _, g := range out {
		if g.Kind == chatsearch.Saved {
			kept := make([]chatsearch.Row, 0, len(g.Rows))
			for _, row := range g.Rows {
				key := chatsearchMessageKey(row)
				if key != "" {
					saved[key] = true
					if listed[key] {
						continue
					}
				}
				kept = append(kept, row)
			}
			if len(kept) == 0 {
				continue
			}
			g.Rows = kept
		}
		result = append(result, g)
	}
	return result, saved
}

// chatsearchHighlightNodes tints the searched words in the plain text among
// nodes; chips, links and other elements are left as they are.
func chatsearchHighlightNodes(words string, nodes []ui.Node) []ui.Node {
	out := make([]ui.Node, 0, len(nodes))
	for _, node := range nodes {
		if node != nil {
			if kind, _ := node.Type.(string); kind == "TEXT_ELEMENT" {
				out = append(out, highlightText(node.TextContent, words)...)
				continue
			}
		}
		out = append(out, node)
	}
	return out
}

// chatsearchPeople maps a person's id to the name one render shows for them.
type chatsearchPeople map[string]string

func chatsearchPeopleOf(m Model) chatsearchPeople {
	people := chatsearchPeople{}
	for _, p := range mentionIndex(m) {
		people[p.id] = p.name
	}
	return people
}

// chatsearchConversation finds the conversation a result sits in.
func chatsearchConversation(m Model, row chatsearch.Row) (Conversation, bool) {
	if row.Target.ConversationID == "" {
		return Conversation{}, false
	}
	for _, c := range m.Conversations {
		if c.ID == row.Target.ConversationID {
			return c, true
		}
	}
	return Conversation{}, false
}

// chatsearchWhere is the small line that opens every result: "in #general", or
// the other person's name for a direct message. It also returns the same words
// as plain text for the control's accessible name. A conversation the page
// cannot name yet is replaced by the kind of result, never by an identifier.
func chatsearchWhere(m Model, row chatsearch.Row) ([]ui.Node, string) {
	c, ok := chatsearchConversation(m, row)
	if !ok {
		label := chatsearchKind(m.Locale, row.Kind)
		return []ui.Node{ui.Text(label)}, label
	}
	name := displayName(m, c)
	if name == "" {
		name = m.t(kindKey(c.Kind))
	}
	var glyph ui.Node
	switch c.Kind {
	case PublicChannel:
		name = "#" + name
	default:
		glyph = kindGlyph(m, c, m.t(kindKey(c.Kind)))
	}
	nodes := searchContext(m, glyph, name)
	return nodes, strings.ReplaceAll(m.t(KeySearchMessageIn), "{channel}", name)
}

// chatsearchResult draws one result.
func chatsearchResult(m Model, people chatsearchPeople, row chatsearch.Row, words string, saved bool) ui.Node {
	locale := m.Locale
	if ChatSearchAgentHref(row) != "" {
		// CHATSEARCH-003: an agent record is on its own page, not in a conversation.
		return chatsearch003Result(locale, row, words)
	}
	target, _ := json.Marshal(row.Target)
	data := map[string]string{"chatsearch-action": "open", "target": base64.RawURLEncoding.EncodeToString(target), "kind": string(row.Kind)}
	whereNodes, wherePlain := chatsearchWhere(m, row)
	label := chatsearchText(locale, "open") + ": " + wherePlain

	var main ui.Node
	text := html.Div(html.Props{Class: "chatsearch-text", Dir: "auto"}, highlightText(chatDisplayText(row.Text), words)...)
	switch {
	case chatsearchMessageKey(row) != "":
		var summary string
		main, summary = chatsearchMessageBody(m, people, row, words, saved)
		if summary != "" {
			label += " · " + summary
		}
	case row.Kind == chatsearch.Person || row.Kind == chatsearch.Conversation:
		// The kind is the line above the name.
		whereNodes = []ui.Node{ui.Text(chatsearchKind(locale, row.Kind))}
		label = chatsearchText(locale, "open") + ": " + chatsearchKind(locale, row.Kind)
		main = html.Div(html.Props{Class: "chatsearch-plain"}, text)
	default:
		line := []ui.Node{html.Strong(html.Props{Class: "chatsearch-kind", Text: chatsearchKind(locale, row.Kind)})}
		if row.Private {
			line = append(line, html.Span(html.Props{Class: "chatsearch-private", Text: chatsearchText(locale, "only")}))
		}
		main = html.Div(html.Props{Class: "chatsearch-plain"}, html.Div(html.Props{Class: "chatsearch-meta"}, line...), text)
	}
	open := html.Button(html.Props{Type: "button", Class: "chatsearch-open", Data: data, Aria: map[string]string{"label": label}}, whereNodes...)
	return html.Div(html.Props{Class: "chatsearch-result", Data: data}, open, main)
}

// chatsearchMessageBody is the avatar, the author and time line and the text of
// a result that is one message, and a short spoken summary of them.
func chatsearchMessageBody(m Model, people chatsearchPeople, row chatsearch.Row, words string, saved bool) (ui.Node, string) {
	locale := m.Locale
	name := people[row.AuthorID]
	conversation, known := chatsearchConversation(m, row)
	if name == "" && known && conversation.Agent {
		name = displayName(m, conversation)
	}
	if name == "" {
		name = chatsearchKind(locale, row.Kind)
	}
	body := chatSearchRowBody(row.Text)
	if row.Kind == chatsearch.Saved {
		// A saved result carries the person's note above the message.
		if _, rest, ok := strings.Cut(body, "\n"); ok {
			body = rest
		}
	}
	if len([]rune(body)) > chatsearchBodyLimit {
		// A long post shows the stretch around the first match; the rest is in
		// the conversation the result opens.
		body = searchSnippet(body, words, chatsearchBodyLimit)
	}
	msg := Message{ID: row.Target.MessageID, AuthorID: row.AuthorID, Author: name, Body: body, SentAt: row.At}
	// A search row carries no references; the message already on the page does,
	// so a mention in a message the page holds is drawn as the chip it is there.
	for _, held := range append(append([]Message{}, m.Messages...), m.ThreadMessages...) {
		if held.ID == msg.ID {
			msg.PersonaReferences = held.PersonaReferences
			break
		}
	}
	rendered := m
	rendered.renderHighlight = words
	text, msg := chatsave002Body(rendered, msg)
	if msg.Author != "" {
		name = msg.Author
	}

	lead := ui.Node(personAvatar(m, row.AuthorID, name, "avatar small chatsearch-avatar"))
	meta := []ui.Node{html.Strong(html.Props{Class: "chatsearch-author", Dir: "auto", Text: name})}
	if msg.PersonaActor != nil && msg.PersonaActor.valid() {
		lead = integrate1MessageAvatar(m, msg, "avatar small chatsearch-avatar")
		meta = append(meta, chatux003MessageBadge(m, msg))
	} else if icon, agent := chatsearchAgentAvatar(m, row, conversation, known, name, "avatar small chatsearch-avatar"); agent {
		// An agent wears its own icon here as it does in the conversation, not
		// the initials of its name.
		lead = icon
	}
	when := ""
	if !row.At.IsZero() {
		when = dayLabel(m, row.At) + " " + chat5Clock(locale, row.At)
		meta = append(meta, html.Time(html.Props{Class: "chatsearch-time", Text: when}))
	}
	if row.Private && row.Kind != chatsearch.Saved {
		meta = append(meta, html.Span(html.Props{Class: "chatsearch-private", Text: chatsearchText(locale, "only")}))
	}
	if saved || row.Kind == chatsearch.Saved {
		copy := SavedMessagesCopy(locale)
		meta = append(meta, html.Span(html.Props{Class: "chatsearch-saved", Title: copy.Saved, Aria: map[string]string{"label": copy.Saved}}, chatsave002Icon("bookmark", true)))
	}
	// In a direct message the conversation is named after the other person, so
	// their name is not spoken a second time as the author.
	summary := name
	if known && conversation.Kind == DirectMessage && name == displayName(m, conversation) {
		summary = ""
	}
	if when != "" {
		if summary != "" {
			summary += " · "
		}
		summary += when
	}
	return html.Div(html.Props{Class: "chatsearch-message"}, lead,
		html.Div(html.Props{Class: "chatsearch-main"},
			html.Div(html.Props{Class: "chatsearch-meta"}, meta...),
			html.Div(html.Props{Class: "chatsearch-text"}, text))), summary
}

// chatsearchBodyLimit is how much of a post a result shows before it is cut
// around the first match.
const chatsearchBodyLimit = 320

// searchHeadCount is the count the results panel's own heading repeats. While
// the search service's results are on the page, the conversation header above
// is the one place the count is printed.
func searchHeadCount(m Model) string {
	if m.ChatSearch != nil {
		return ""
	}
	return searchCountLabel(m)
}
