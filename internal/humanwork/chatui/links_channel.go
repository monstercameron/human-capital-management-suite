package chatui

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type ChannelReference struct {
	ID         string
	Start, End int
}

var channelURLPattern = regexp.MustCompile(`https?://[^\s<>"']+|/workspace/app/chat#channel=[^\s<>"']+`)

func validChannelReferenceID(id string) bool {
	if id == "" || len(id) > 256 || !utf8.ValidString(id) || strings.TrimSpace(id) == "" {
		return false
	}
	for _, r := range id {
		if unicode.IsControl(r) || strings.ContainsRune("<>\"'", r) {
			return false
		}
	}
	return true
}

// ChannelReferenceLabel formats only the copied label; stored conversation
// names remain untouched.
func ChannelReferenceLabel(name string) string { return "#" + strings.Join(strings.Fields(name), "_") }

func ChannelReferenceURL(id string) string {
	return "/workspace/app/chat#channel=" + url.QueryEscape(id)
}

// ChannelReferences accepts canonical same-origin chat URLs only. It never
// infers a room from bare #text or from an untrusted host.
func ChannelReferences(body, origin string) []ChannelReference {
	if len(body) > 32*1024 {
		body = body[:32*1024]
	}
	base, err := url.Parse(origin)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil
	}
	var out []ChannelReference
	for _, span := range channelURLPattern.FindAllStringIndex(body, 8) {
		if span[0] > 0 && !strings.ContainsRune(" \t\r\n([{\"'", rune(body[span[0]-1])) {
			continue
		}
		full := body[span[0]:span[1]]
		for _, match := range []string{strings.TrimRight(full, ".,!?;:)]}>"), full} {
			parsed, err := url.Parse(match)
			if err != nil || parsed.Path != "/workspace/app/chat" || parsed.RawQuery != "" {
				continue
			}
			if parsed.IsAbs() && (parsed.Scheme != base.Scheme || !strings.EqualFold(parsed.Host, base.Host)) {
				continue
			}
			fragment := parsed.EscapedFragment()
			if !strings.HasPrefix(fragment, "channel=") {
				continue
			}
			id, err := url.QueryUnescape(strings.TrimPrefix(fragment, "channel="))
			if err != nil || !validChannelReferenceID(id) || "/workspace/app/chat#"+fragment != ChannelReferenceURL(id) {
				continue
			}
			out = append(out, ChannelReference{ID: id, Start: span[0], End: span[0] + len(match)})
			break
		}
	}
	return out
}

func admittedChannelReference(m Model, id string) (Conversation, bool) {
	for _, conversation := range m.Conversations {
		if conversation.ID == id && conversation.Joined {
			return conversation, true
		}
	}
	return Conversation{}, false
}

// channelReferenceBody turns copied "#label URL" pairs into a single link.
// A room absent from the reader's admitted list gets a generic link; the
// receiver must authorize it before showing a name or opening the room.
func channelReferenceBody(m Model, body string) []ui.Node {
	refs := ChannelReferences(body, m.EmbedOrigin)
	if len(refs) == 0 {
		return []ui.Node{ui.Text(body)}
	}
	var nodes []ui.Node
	last := 0
	for _, ref := range refs {
		conversation, ok := admittedChannelReference(m, ref.ID)
		if ref.Start < last {
			continue
		}
		if !ok {
			start := ref.Start
			prefix := body[last:start]
			if strings.HasSuffix(prefix, " ") {
				withoutSpace := prefix[:len(prefix)-1]
				wordStart := strings.LastIndexAny(withoutSpace, " \t\r\n") + 1
				word := withoutSpace[wordStart:]
				if len(word) > 1 && len(word) <= 128 && strings.HasPrefix(word, "#") {
					start -= len(word) + 1
				}
			}
			if start > last {
				nodes = append(nodes, ui.Text(body[last:start]))
			}
			nodes = append(nodes, html.A(html.Props{Class: "chat-channel-reference", Href: ChannelReferenceURL(ref.ID), Aria: map[string]string{"label": m.t(KeyOpenConversations)}}, ui.Text("#channel")))
			last = ref.End
			continue
		}
		label := conversationReferenceLabel(m, conversation)
		start := ref.Start
		prefix := body[last:start]
		if strings.HasSuffix(prefix, label+" ") {
			start -= len(label) + 1
		}
		if start > last {
			nodes = append(nodes, ui.Text(body[last:start]))
		}
		nodes = append(nodes, html.A(html.Props{Class: "chat-channel-reference", Href: ChannelReferenceURL(ref.ID), Data: map[string]string{"action": "open-channel-reference", "id": ref.ID}}, ui.Text(label)))
		last = ref.End
	}
	if last < len(body) {
		nodes = append(nodes, ui.Text(body[last:]))
	}
	if len(nodes) == 0 {
		return []ui.Node{ui.Text(body)}
	}
	return nodes
}

// channelReferenceLinks offers a small preview for a draft without changing
// the editable composer text.
func channelReferenceLinks(m Model, body string) []ui.Node {
	var nodes []ui.Node
	for _, ref := range ChannelReferences(body, m.EmbedOrigin) {
		conversation, ok := admittedChannelReference(m, ref.ID)
		if !ok {
			nodes = append(nodes, html.A(html.Props{Class: "chat-channel-reference", Href: ChannelReferenceURL(ref.ID), Aria: map[string]string{"label": m.t(KeyOpenConversations)}}, ui.Text("#channel")))
			continue
		}
		nodes = append(nodes, html.A(html.Props{Class: "chat-channel-reference", Href: ChannelReferenceURL(ref.ID), Data: map[string]string{"action": "open-channel-reference", "id": ref.ID}}, ui.Text(conversationReferenceLabel(m, conversation))))
	}
	return nodes
}
