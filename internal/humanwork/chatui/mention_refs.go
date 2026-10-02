package chatui

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type mentionTarget struct {
	id, name string
	agent    *ResolvedPersonaMention
}

func canonicalMentionIndex(m Model) []mentionTarget {
	out := make([]mentionTarget, 0, len(m.renderReferences))
	for _, ref := range m.renderReferences {
		if strings.TrimSpace(ref.ID) == "" || strings.TrimSpace(ref.Display) == "" {
			continue
		}
		target := mentionTarget{id: ref.ID, name: strings.TrimSpace(ref.Display)}
		if ref.Kind == "AGENT_MENTION" {
			for i := range m.ResolvedPersonaMentions {
				persona := &m.ResolvedPersonaMentions[i]
				if persona.Reference.ID == ref.ID && (ref.TenantID == "" || persona.Reference.TenantID == ref.TenantID) {
					target.agent = persona
					break
				}
			}
		}
		out = append(out, target)
	}
	sort.SliceStable(out, func(i, j int) bool { return len(out[i].name) > len(out[j].name) })
	return out
}

// mentionIndex maps the people this view can name to their subject IDs,
// longest name first, so "@Ana Maria Lopez" beats "@Ana Maria".
func mentionIndex(m Model) []mentionTarget {
	seen := map[string]bool{}
	var out []mentionTarget
	add := func(id, name string) {
		name = strings.TrimSpace(name)
		key := strings.ToLower(name)
		if id == "" || name == "" || name == id || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, mentionTarget{id: id, name: name})
	}
	add(m.CurrentUser, m.CurrentUserName)
	for _, p := range m.Members {
		add(p.ID, p.Name)
	}
	for _, list := range [][]Message{m.Messages, m.ThreadMessages} {
		for _, msg := range list {
			add(msg.AuthorID, msg.Author)
		}
	}
	for _, p := range m.SearchDirectory {
		add(p.ID, p.Name)
	}
	sort.SliceStable(out, func(i, j int) bool { return len(out[i].name) > len(out[j].name) })
	return out
}

// mentionReferenceBody turns "@Full Name" into a person chip that opens the
// person's details, then hands the text between chips to the channel
// reference renderer. A name nobody resolves stays plain text.
func mentionReferenceBody(m Model, body string) []ui.Node {
	body = directAgentQuestionBody(m, body)
	// CHATBUG-037: a share address is a short link, never the raw token.
	if nodes, ok := chatbug037ShareBody(m, body, mentionReferenceRuns); ok {
		return nodes
	}
	return mentionReferenceRuns(m, body)
}

func mentionReferenceRuns(m Model, body string) []ui.Node {
	if !strings.Contains(body, "@") {
		return journeyReferenceBody(m, body)
	}
	index := canonicalMentionIndex(m)
	if len(index) == 0 {
		return journeyReferenceBody(m, body)
	}
	var nodes []ui.Node
	last := 0
	for i := 0; i < len(body); i++ {
		if body[i] != '@' {
			continue
		}
		if i > 0 {
			prev, _ := utf8.DecodeLastRuneInString(body[:i])
			if unicode.IsLetter(prev) || unicode.IsDigit(prev) || prev == '_' {
				continue
			}
		}
		rest := body[i+1:]
		for _, target := range index {
			if len(rest) < len(target.name) || !strings.EqualFold(rest[:len(target.name)], target.name) {
				continue
			}
			if after, _ := utf8.DecodeRuneInString(rest[len(target.name):]); len(rest) > len(target.name) && (unicode.IsLetter(after) || unicode.IsDigit(after) || after == '_') {
				continue
			}
			if i > last {
				nodes = append(nodes, journeyReferenceBody(m, body[last:i])...)
			}
			nodes = append(nodes, mentionChip(m, target))
			last = i + 1 + len(target.name)
			i = last - 1
			break
		}
	}
	if last < len(body) {
		nodes = append(nodes, journeyReferenceBody(m, body[last:])...)
	}
	return nodes
}

// A direct conversation already names its agent in the header and composer.
// Older posts may still carry the redundant textual mention; hide only the
// canonical agent prefix and leave the person's question untouched.
func directAgentQuestionBody(m Model, body string) string {
	conversation := m.selected()
	if !conversation.Agent {
		return body
	}
	name := strings.TrimSpace(displayName(m, conversation))
	if name == "" {
		return body
	}
	prefix := "@" + name
	trimmed := strings.TrimLeftFunc(body, unicode.IsSpace)
	if len(trimmed) < len(prefix) || !strings.EqualFold(trimmed[:len(prefix)], prefix) {
		return body
	}
	if len(trimmed) > len(prefix) {
		next, _ := utf8.DecodeRuneInString(trimmed[len(prefix):])
		if !unicode.IsSpace(next) {
			return body
		}
	}
	return strings.TrimSpace(trimmed[len(prefix):])
}

func mentionChip(m Model, target mentionTarget) ui.Node {
	class := "mention-chip"
	if target.agent != nil {
		class += " mention-chip-agent"
		label := agentReplyFallback(m.Locale, "chat.agent.badge", "Agent")
		return html.Span(html.Props{Class: "mention-chip-details"},
			html.Button(html.Props{Class: class, Type: "button", Data: map[string]string{"action": "agent-profile-open", "id": target.id}, Aria: map[string]string{"label": target.name + ", " + label, "haspopup": "dialog"}}, ui.Text("@"+target.name), html.Span(html.Props{Class: "sr-only mention-chip-agent-badge", Text: label})))
	}
	if target.id == m.CurrentUser {
		class += " self"
	}
	return html.Button(html.Props{Class: class, Type: "button", Disabled: m.Callbacks.OpenPerson == nil,
		Data: map[string]string{"action": "open-person", "id": target.id},
		Aria: map[string]string{"label": m.tf(KeyViewPerson, map[string]string{"name": target.name})}}, ui.Text("@"+target.name))
}

// chatKnownName is the display name chat already uses for a subject: a room
// member, a message author or the directory's search projection.
func chatKnownName(m Model, id string) string {
	if id == "" {
		return ""
	}
	if id == m.CurrentUser && strings.TrimSpace(m.CurrentUserName) != "" {
		return strings.TrimSpace(m.CurrentUserName)
	}
	for _, p := range m.Members {
		if p.ID == id && p.Name != "" && p.Name != id {
			return p.Name
		}
	}
	for _, list := range [][]Message{m.Messages, m.ThreadMessages} {
		for _, msg := range list {
			if msg.AuthorID == id && msg.Author != "" && msg.Author != id {
				return msg.Author
			}
		}
	}
	for _, p := range m.SearchDirectory {
		if p.ID == id && p.Name != "" {
			return p.Name
		}
	}
	return ""
}
