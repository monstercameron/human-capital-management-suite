package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// SearchFilters reads Slack-style filters out of a chat search: "in:#general"
// narrows to one conversation the viewer has, "from:@Full Name" to one author.
// It returns the remaining text and the resolved IDs. Unknown filters stay in
// the text, and a search that would be left with no text keeps its original
// words, because the server refuses an empty query.
func SearchFilters(m Model, query string) (text, conversationID, authorID string) {
	rest := query
	lower := strings.ToLower(rest)
	if i := strings.Index(lower, "in:"); i >= 0 && (i == 0 || rest[i-1] == ' ') {
		name := rest[i+3:]
		name = strings.TrimPrefix(name, "#")
		end := strings.IndexByte(name, ' ')
		if end < 0 {
			end = len(name)
		}
		want := strings.ToLower(strings.TrimSpace(name[:end]))
		for _, c := range m.Conversations {
			if want != "" && strings.ToLower(displayName(m, c)) == want || strings.ToLower(c.Name) == want && want != "" {
				conversationID = c.ID
				consumed := len("in:") + (len(rest[i+3:]) - len(name)) + end
				rest = rest[:i] + rest[i+consumed:]
				break
			}
		}
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

// searchFilterBar offers the room the viewer came from as a one-click
// "in:#channel" filter and says which filters the box understands.
func searchFilterBar(m Model, query string) ui.Node {
	children := []ui.Node{}
	c := m.selected()
	if m.SelectedID != "" && (c.Kind == PublicChannel || c.Kind == PrivateChannel) && !strings.Contains(strings.ToLower(query), "in:") {
		name := displayName(m, c)
		children = append(children, html.Button(html.Props{Class: "tray-chip search-filter-chip", Type: "button", Disabled: m.Callbacks.Search == nil,
			Data: map[string]string{"action": "search-in-channel", "extra": name}}, icon("search"), html.Span(html.Props{Text: m.tf(KeySearchInChannel, map[string]string{"channel": "#" + name})})))
	}
	children = append(children, html.Span(html.Props{Class: "search-filter-hint", Text: m.t(KeySearchFilterHint)}))
	return html.Div(html.Props{Class: "search-filter-bar"}, children...)
}
