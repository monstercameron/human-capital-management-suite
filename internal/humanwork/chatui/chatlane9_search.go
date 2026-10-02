package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
)

// chatsearchAgentAvatar is the agent's own icon for a result an agent wrote,
// drawn by the function the conversation's messages use, or false when the
// author is a person. A result carries no persona actor, so the author is known
// to be an agent from the result itself (an answer, or marked as by an agent),
// from the conversation it sits in (the agent a direct message is with), or from
// the roster.
func chatsearchAgentAvatar(m Model, row chatsearch.Row, conversation Conversation, known bool, name, class string) (ui.Node, bool) {
	ids := []string{row.AuthorID}
	isAgent := row.Kind == chatsearch.AgentAnswer || row.ByAgent
	var stored agenticon.Value
	if known && conversation.Agent && (row.AuthorID == "" || row.AuthorID == conversation.AgentID) {
		isAgent = true
		ids = append(ids, conversation.AgentID)
		stored = conversation.Icon
	}
	for _, member := range m.Members {
		if member.Agent && member.ID == row.AuthorID {
			isAgent = true
			if !stored.Valid() {
				stored = member.Icon
			}
		}
	}
	if !isAgent {
		return nil, false
	}
	return agentDMAvatar(name, class+" agent-dm-avatar", agentIconFor(m, ids, name, stored)), true
}

// chatsearchRecentMax is how many recent searches the list under the box shows.
const chatsearchRecentMax = 5

// chatsearchRecentList is the small list under the search box that offers the
// person's recent searches while the box is focused and empty. It is drawn only
// while the box is empty and the style shows it only while the box holds the
// cursor (:focus-within), so it needs no state of its own. Pressing one fills
// the box and searches for it.
func chatsearchRecentList(m Model) ui.Node {
	if strings.TrimSpace(m.Search) != "" || len(m.SearchRecent) == 0 {
		return nil
	}
	items := make([]ui.Node, 0, chatsearchRecentMax)
	for _, query := range m.SearchRecent {
		if query = strings.TrimSpace(query); query == "" || query == "*" {
			continue
		}
		items = append(items, html.WithKey(html.Button(html.Props{Class: "rail-search-recent-item", Type: "button", Dir: "auto", Title: query,
			Data: map[string]string{"action": "search-recent", "id": query}}, icon("search"), html.Span(html.Props{Text: query})), "recent:"+query))
		if len(items) == chatsearchRecentMax {
			break
		}
	}
	if len(items) == 0 {
		return nil
	}
	label := chatsearchText(m.Locale, "recent")
	return html.Div(html.Props{Class: "rail-search-recent", Role: "group", Aria: map[string]string{"label": label}},
		append([]ui.Node{html.Span(html.Props{Class: "rail-search-recent-title", Text: label})}, items...)...)
}
