package main

import (
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// CHATSEARCH-002: the workspace search box asks Chat too. The rows Chat
// answers with are listed after the workspace's own results, each with the
// kind of thing it is, and each opens Chat at the exact place.

// chatsearchWorkspaceLimit is how many Chat rows the workspace list shows, the
// same as it allows any one kind of its own.
const chatsearchWorkspaceLimit = 4

// chatsearchWorkspaceRequest is the search the workspace box sends for words:
// by keyword, a few rows, and not remembered as a recent Chat search, because
// it is sent as the person types.
func chatsearchWorkspaceRequest(model chatui.Model, words string) (chatsearch.Request, error) {
	resolved, err := chatui.ResolveChatSearchQuery(model, words)
	if err != nil {
		return chatsearch.Request{}, err
	}
	// More rows than are shown are asked for: people are left out of the list
	// and one message can be returned by several sources.
	return chatsearch.Request{Query: resolved, DisplayQuery: words, Mode: "keyword", Limit: 4 * chatsearchWorkspaceLimit, Transient: true}, nil
}

// chatsearchWorkspaceItems turns Chat's answer into rows of the workspace
// search list. An agent record carries the Agents page's icon and every other
// row Chat's.
func chatsearchWorkspaceItems(model chatui.Model, response chatsearch.Response, words string) []productui.GlobalSearchItem {
	chat, _ := productui.LookupPage(productui.PageChat)
	agents, _ := productui.LookupPage(productui.PageAgents)
	items := []productui.GlobalSearchItem{}
	for _, result := range chatui.ChatSearchWorkspaceResults(model, response, words, chatsearchWorkspaceLimit) {
		icon := chat.Icon
		switch result.Kind {
		case chatsearch.Agent, chatsearch.AgentTask, chatsearch.AgentAnnouncement:
			icon = agents.Icon
		}
		items = append(items, productui.GlobalSearchItem{ID: "chat:" + result.ID, Kind: "chat", KindLabel: result.KindLabel, Label: result.Text, Description: result.Where, Href: result.Href, Icon: icon})
	}
	return items
}
