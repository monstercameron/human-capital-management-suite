package chatui

import (
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// The preview of a to-do list says which tasks have no owner because a name
// fit nobody here, and lets the author pick one of this conversation's
// members or keep the word as text. A choice is the setting "assign-N" (N is
// the unresolved name's place in the draft): a member's id, or "text".

var chatcmd003Issue3984Copy = map[string][3]string{
	"unassigned": {"“{token}” does not match anyone in this conversation, so “{task}” has no owner yet. Pick a person below or keep it as text.",
		"„{token}“ passt zu niemandem in dieser Unterhaltung, deshalb hat „{task}“ noch keine zuständige Person. Wählen Sie unten eine Person oder behalten Sie es als Text.",
		"«{token}» لا يطابق أحدًا في هذه المحادثة، لذا لا مسؤول عن «{task}» بعد. اختر شخصًا أدناه أو أبقِه نصًا."},
	"assign-label": {"Owner for “{task}”", "Zuständig für „{task}“", "المسؤول عن «{task}»"},
	"assign-text":  {"Keep {token} as text, no owner", "{token} als Text behalten, ohne Zuständige", "إبقاء {token} نصًا دون مسؤول"},
}

func init() {
	for key, value := range chatcmd003Issue3984Copy {
		chatcmd003Copy[key] = value
	}
}

// chatcmd003Assign gives each unresolved name the member the author chose for
// it. The card and the changes of draft are changed in place.
func chatcmd003Assign(m Model, draft *chat.Chatcmd003Draft, settings map[string]string) {
	for i, u := range draft.Unresolved {
		id := settings["assign-"+strconv.Itoa(i)]
		if id == "" || id == "text" {
			continue
		}
		for _, member := range m.Members {
			if member.ID != id {
				continue
			}
			if chat.Chatcmd004AssignUnresolved(&draft.Card, u, chat.Chatcmd004Member{HomeTenantID: member.HomeTenantID, ID: member.ID, Name: member.Name}) {
				draft.Changes = chatcmd003DropOwnerChange(draft.Changes, u)
				draft.Changes = append(draft.Changes, chat.Chatcmd003Change{Kind: chat.Chatcmd004ChangeAssignee, Before: draft.Card.Todo.Items[u.Item].Text, After: member.Name})
			}
			break
		}
	}
}

// chatcmd003Candidates are the members offered for a name: the ones whose
// name shares its beginning or contains it, else the first few by name. No
// one is preselected.
func chatcmd003Candidates(m Model, token string) []Member {
	word := strings.ToLower(strings.TrimLeft(token, "@"))
	var near, rest []Member
	for _, member := range m.Members {
		name := strings.ToLower(member.Name)
		first, _, _ := strings.Cut(name, " ")
		shared := 0
		for shared < len(word) && shared < len(first) && word[shared] == first[shared] {
			shared++
		}
		if word != "" && (shared >= 2 || strings.Contains(name, word)) {
			near = append(near, member)
		} else {
			rest = append(rest, member)
		}
	}
	sort.SliceStable(rest, func(i, j int) bool { return strings.ToLower(rest[i].Name) < strings.ToLower(rest[j].Name) })
	out := append(near, rest...)
	if len(near) > 0 && len(out) > 6 {
		out = out[:6]
	} else if len(out) > 8 {
		out = out[:8]
	}
	return out
}

// chatcmd003Unresolved is the preview's part for names that fit nobody: a
// status that the task is unassigned, and the choice of owner.
func chatcmd003Unresolved(m Model, state chatcmd003Preview) []ui.Node {
	var nodes []ui.Node
	todo := state.Draft.Card.Todo
	for i, u := range state.Draft.Unresolved {
		if todo == nil || u.Item >= len(todo.Items) {
			continue
		}
		chosen := state.Settings["assign-"+strconv.Itoa(i)]
		task := excerpt(chatcmd003TypedText(state, u), 60)
		if chosen == "" || chosen == "text" {
			nodes = append(nodes, html.P(html.Props{Class: "chatcmd003-note chatcmd003-unassigned", Role: "status", Dir: "auto",
				Text: chatcmd003UnassignedText(m, u, task)}))
		}
		labelID := "chatcmd003-assign-" + strconv.Itoa(i) + "-label"
		choice := func(id, text string) ui.Node {
			class := "chatcmd003-choice"
			selected := id == chosen || (id == "text" && chosen == "")
			if selected {
				class += " selected"
			}
			return html.Button(html.Props{Class: class, Type: "button", Role: "radio", Disabled: state.Busy, Dir: "auto",
				Data: map[string]string{"action": "chatcmd003-set", "id": "assign-" + strconv.Itoa(i), "extra": id},
				Aria: map[string]string{"checked": boolString(selected)}, Text: text})
		}
		buttons := []ui.Node{}
		for _, member := range chatcmd003Candidates(m, u.Token) {
			buttons = append(buttons, choice(member.ID, member.Name))
		}
		buttons = append(buttons, choice("text", chatcmd003KeepText(m, u)))
		nodes = append(nodes, html.Div(html.Props{ID: "chatcmd003-assign-" + strconv.Itoa(i), Class: "chatcmd003-setting", Role: "radiogroup", Aria: map[string]string{"labelledby": labelID}},
			html.Span(html.Props{ID: labelID, Class: "chatcmd003-setting-label", Dir: "auto", Text: chatcmd002Fill(chatcmd003Text(m, "assign-label"), "task", task)}),
			html.Div(html.Props{Class: "chatcmd003-choices"}, buttons...)))
	}
	return nodes
}

// chatcmd003TypedText is the task's text with the unresolved name in it, for
// the label of a choice already made.
func chatcmd003TypedText(state chatcmd003Preview, u chat.Chatcmd004Unresolved) string {
	if o := state.Draft.Original.Todo; o != nil && u.Item < len(o.Items) {
		return o.Items[u.Item].Text
	}
	return ""
}
