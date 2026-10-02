package chatui

import (
	"strings"
	"unicode/utf8"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// SearchFilters reads Slack-style filters out of a chat search: "in:#general"
// narrows to one conversation the viewer has, "from:@Full Name" to one author.
// A channel whose name holds spaces is quoted, in:"General Chat", and a name
// typed without quotes is still found by matching the channels' own names.
// It returns the remaining text and the resolved IDs. Unknown filters stay in
// the text, and a search that would be left with no text keeps its original
// words, because the server refuses an empty query.
func SearchFilters(m Model, query string) (text, conversationID, authorID string) {
	rest := query
	lower := strings.ToLower(rest)
	for from := 0; from < len(lower); {
		at := strings.Index(lower[from:], "in:")
		if at < 0 {
			break
		}
		i := from + at
		from = i + len("in:")
		if i != 0 && rest[i-1] != ' ' {
			continue
		}
		name := strings.TrimPrefix(rest[i+len("in:"):], "#")
		hash := len(rest[i+len("in:"):]) - len(name)
		id, used := channelFilterTarget(m, name)
		if id == "" {
			continue
		}
		conversationID = id
		rest = rest[:i] + rest[i+len("in:")+hash+used:]
		break
	}
	lower = strings.ToLower(rest)
	if i := strings.Index(lower, "from:@"); i >= 0 && (i == 0 || rest[i-1] == ' ') {
		tail := rest[i+len("from:@"):]
		for _, target := range mentionIndex(m) {
			if len(tail) >= len(target.name) && strings.EqualFold(tail[:len(target.name)], target.name) {
				authorID = target.id
				rest = rest[:i] + tail[len(target.name):]
				break
			}
		}
	}
	text = strings.Join(strings.Fields(rest), " ")
	if text == "" {
		text = strings.TrimSpace(query)
	}
	return text, conversationID, authorID
}

// channelFilterTarget resolves what follows "in:#": a quoted name, or the
// longest conversation name the text starts with, ending at a space or the end
// of the text. It returns the conversation and how many bytes of s it used.
func channelFilterTarget(m Model, s string) (id string, used int) {
	if strings.HasPrefix(s, `"`) {
		inner, _, closed := strings.Cut(s[1:], `"`)
		used = 1 + len(inner)
		if closed {
			used++
		}
		want := strings.TrimSpace(inner)
		for _, c := range m.Conversations {
			if want != "" && channelNamedLike(m, c, want) {
				return c.ID, used
			}
		}
		return "", 0
	}
	for _, c := range m.Conversations {
		for _, name := range []string{displayName(m, c), c.Name} {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if n, ok := foldPrefix(s, name); ok && n > used && (n == len(s) || s[n] == ' ') {
				id, used = c.ID, n
			}
		}
	}
	return id, used
}

func channelNamedLike(m Model, c Conversation, want string) bool {
	return strings.EqualFold(strings.TrimSpace(displayName(m, c)), want) || strings.EqualFold(strings.TrimSpace(c.Name), want)
}

// foldPrefix reports whether s starts with name, ignoring case, and how many
// bytes of s that covers.
func foldPrefix(s, name string) (int, bool) {
	at := 0
	for _, want := range name {
		if at >= len(s) {
			return 0, false
		}
		got, size := utf8.DecodeRuneInString(s[at:])
		if !strings.EqualFold(string(got), string(want)) {
			return 0, false
		}
		at += size
	}
	return at, true
}

// searchFilterBar offers the room the viewer came from as a one-click
// "in:#channel" filter and says which filters the box understands. The chip is
// an offer ("Search only in #name") until pressed; once the query carries the
// filter the offer goes away and the results show it as a removable filter.
func searchFilterBar(m Model, query string) ui.Node {
	children := []ui.Node{}
	c := m.selected()
	if m.SelectedID != "" && (c.Kind == PublicChannel || c.Kind == PrivateChannel) && !strings.Contains(strings.ToLower(query), "in:") {
		name := displayName(m, c)
		children = append(children, html.Button(html.Props{Class: "tray-chip search-filter-chip", Type: "button", Disabled: m.Callbacks.Search == nil,
			Data: map[string]string{"action": "search-in-channel", "id": c.ID, "extra": searchInToken(name)}}, icon("search"), html.Span(html.Props{Text: laneTextf(m, chatbug072Copy, keyChatbug072ScopeOffer, map[string]string{"channel": "#" + name})})))
	}
	children = append(children, html.Span(html.Props{Class: "search-filter-hint", Text: m.t(KeySearchFilterHint)}))
	children = append(children, ChatSearchMount(m))
	return html.Div(html.Props{Class: "search-filter-bar"}, children...)
}
