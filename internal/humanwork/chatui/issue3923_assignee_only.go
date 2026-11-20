package chatui

import (
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// Where only the assignee may complete items, an item with no assignee could
// never be completed (#3923). The preview says so for each such item, keeps
// Post off, and lets the author pick an owner from this conversation's members
// or choose, by pressing a button, that anyone may complete items. A choice is
// the setting "pick-N" (N is the item's place in the list): a member's id.
// Nobody is added to the conversation and nobody is chosen for the author.

var chatcmd003Issue3923Copy = map[string][3]string{
	"assignee-needed": {"Only the assignee can complete items, and some items have no assignee. Choose who completes each one below, or let anyone complete items.",
		"Nur die zuständige Person kann Aufgaben abhaken, und einige Aufgaben haben keine. Wählen Sie unten, wer sie erledigt, oder erlauben Sie allen, Aufgaben abzuhaken.",
		"المسؤول وحده يستطيع إكمال العناصر، وبعض العناصر بلا مسؤول. اختر أدناه من يُكمل كل عنصر، أو اسمح لأي شخص بإكمال العناصر."},
	"pick-note":   {"“{task}” has no owner, so nobody could complete it.", "„{task}“ hat keine zuständige Person, deshalb könnte niemand es abhaken.", "«{task}» بلا مسؤول، لذا لا يستطيع أحد إكماله."},
	"pick-label":  {"Who completes “{task}”?", "Wer erledigt „{task}“?", "من يُكمل «{task}»؟"},
	"pick-anyone": {"Let anyone complete items", "Allen erlauben, Aufgaben abzuhaken", "السماح لأي شخص بإكمال العناصر"},
	"assignee-leaves": {"If that person leaves this conversation, nobody else can complete the item, and no one is given access automatically.",
		"Wenn diese Person die Unterhaltung verlässt, kann niemand sonst die Aufgabe abhaken, und es erhält auch niemand automatisch Zugriff.",
		"إذا غادر هذا الشخص المحادثة فلن يستطيع أحد غيره إكمال العنصر، ولن يُمنح أي شخص صلاحية تلقائيًا."},
}

func init() {
	for key, value := range chatcmd003Issue3923Copy {
		chatcmd003Copy[key] = value
	}
}

// chatcmd003Picks gives each item the author picked an owner for that owner.
// Only members of this conversation can be picked.
func chatcmd003Picks(m Model, c *chat.Chatcmd002Card, settings map[string]string) {
	if c == nil || c.Todo == nil {
		return
	}
	for i := range c.Todo.Items {
		id := settings["pick-"+strconv.Itoa(i)]
		if id == "" {
			continue
		}
		for _, member := range m.Members {
			if member.ID == id {
				item := &c.Todo.Items[i]
				item.AssigneeHomeTenantID, item.AssigneeID, item.AssigneeName = member.HomeTenantID, member.ID, member.Name
				break
			}
		}
	}
}

// chatcmd003AssigneeOnly is the preview's part for a list that only assignees
// may complete: for each item without one, what that means and who to pick,
// then the alternative, and last what happens when an assignee leaves.
func chatcmd003AssigneeOnly(m Model, state chatcmd003Preview) []ui.Node {
	todo := state.Draft.Card.Todo
	if todo == nil || todo.Tick != "assignee" {
		return nil
	}
	var nodes []ui.Node
	gaps := false
	for i, item := range todo.Items {
		group, task := "pick-"+strconv.Itoa(i), excerpt(item.Text, 60)
		if item.AssigneeID != "" && state.Settings[group] == "" {
			continue
		}
		gaps = true
		if item.AssigneeID == "" {
			nodes = append(nodes, html.P(html.Props{Class: "chatcmd003-note chatcmd003-unassigned", Role: "status", Dir: "auto", Text: chatcmd002Fill(chatcmd003Text(m, "pick-note"), "task", task)}))
		}
		// An item the author picked an owner for keeps its choice, so the pick can
		// be changed and focus stays on the button just pressed.
		var keys, texts []string
		for _, member := range chatcmd003Candidates(m, "") {
			keys, texts = append(keys, member.ID), append(texts, member.Name)
		}
		if len(keys) > 0 {
			nodes = append(nodes, chatcmd003ReviewGroup(m, state, group, chatcmd002Fill(chatcmd003Text(m, "pick-label"), "task", task), state.Settings[group], keys, texts))
		}
	}
	if gaps {
		nodes = append(nodes, html.Button(html.Props{ID: "chatcmd003-pick-anyone", Class: "button secondary small", Type: "button", Disabled: state.Busy,
			Data: map[string]string{"action": "chatcmd003-set", "id": "tick", "extra": "anyone"}, Text: chatcmd003Text(m, "pick-anyone")}))
	}
	return append(nodes, html.P(html.Props{Class: "chatcmd003-note", Dir: "auto", Text: chatcmd003Text(m, "assignee-leaves")}))
}

// chatcmd003AssigneeGap reports whether the card is a list only assignees may
// complete with an item that has none.
func chatcmd003AssigneeGap(c chat.Chatcmd002Card) bool {
	return strings.TrimSpace(c.Kind) == "todo" && c.AssigneeOnlyGap() >= 0
}
