package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// chatux020Position is where a conversation sits in the section the person
// ordered, and false when it has no order of its own to change: a favorite keeps
// the order of the section it came from (Up and Down there would move it where
// nothing shows the change), and a rail with no saved layout is sorted by the
// page itself.
func chatux020Position(m Model, id string) (position, count int, ok bool) {
	if len(m.Sections) == 0 {
		return 0, 0, false
	}
	for _, section := range chatside001Sections(m, m.Sections) {
		if section.ID == chatside001Favorites {
			continue
		}
		for index, conversation := range section.Chats {
			if conversation.ID == id {
				return index, len(section.Chats), true
			}
		}
	}
	return 0, 0, false
}

// chatux020Reorder is the pair at the foot of the row menu, after a divider:
// Move conversation up and down, each offered only where the row has somewhere
// to go. It is nil when the row's section is sorted automatically, or when there
// is nowhere to go either way.
func chatux020Reorder(m Model, id string) []ui.Node {
	position, count, ok := chatux020Position(m, id)
	if !ok {
		return nil
	}
	headingID := "rail-menu-reorder-heading"
	group := []ui.Node{}
	if position > 0 {
		group = append(group, html.Button(html.Props{Class: "menu-item", Type: "button", Role: "menuitem", Disabled: m.Callbacks.MoveConversationOrder == nil, Data: map[string]string{"action": "rail-chat-up", "id": id}}, html.Span(html.Props{Text: m.t(KeyMoveConversationUp)})))
	}
	if position < count-1 {
		group = append(group, html.Button(html.Props{Class: "menu-item", Type: "button", Role: "menuitem", Disabled: m.Callbacks.MoveConversationOrder == nil, Data: map[string]string{"action": "rail-chat-down", "id": id}}, html.Span(html.Props{Text: m.t(KeyMoveConversationDown)})))
	}
	if len(group) == 0 {
		return nil
	}
	heading := html.Div(html.Props{ID: headingID, Class: "menu-heading", Text: laneText(m, chatux020Copy, keyChatux020Reorder)})
	return []ui.Node{
		html.Div(html.Props{Class: "menu-separator", Role: "separator"}),
		html.Div(html.Props{Class: "menu-reorder", Role: "group", Aria: map[string]string{"labelledby": headingID}}, append([]ui.Node{heading}, group...)...),
	}
}
