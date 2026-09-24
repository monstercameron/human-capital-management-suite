package chatui

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type mentionTarget struct{ id, name string }

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
	if !strings.Contains(body, "@") {
		return docLinkReferenceBody(m, body)
	}
	index := m.mentions
	if !m.mentionsReady {
		index = mentionIndex(m)
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
				nodes = append(nodes, docLinkReferenceBody(m, body[last:i])...)
			}
			nodes = append(nodes, mentionChip(m, target))
			last = i + 1 + len(target.name)
			i = last - 1
			break
		}
	}
	if last < len(body) {
		nodes = append(nodes, docLinkReferenceBody(m, body[last:])...)
	}
	return nodes
}

func mentionChip(m Model, target mentionTarget) ui.Node {
	class := "mention-chip"
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
