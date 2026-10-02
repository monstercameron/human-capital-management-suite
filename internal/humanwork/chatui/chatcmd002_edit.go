package chatui

import (
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// The two ways a card changes after it is posted besides votes and ticks
// (CHATCMD-002): any member adds an option to a poll that lets them, and the
// author rewords the card until its first vote or tick. Both go to the server
// as mutations of the post (ADD_OPTION and EDIT); the page only offers what the
// reader's view of the card says the server will accept.

var chatcmd002EditCopy = map[string][3]string{
	"add-option":    {"Add an option", "Antwort hinzufügen", "إضافة خيار"},
	"add":           {"Add", "Hinzufügen", "إضافة"},
	"edit-poll":     {"Edit poll", "Umfrage bearbeiten", "تعديل الاستطلاع"},
	"edit-list":     {"Edit list", "Liste bearbeiten", "تعديل القائمة"},
	"save":          {"Save changes", "Änderungen speichern", "حفظ التغييرات"},
	"poll-title":    {"Question", "Frage", "السؤال"},
	"list-title":    {"Title", "Titel", "العنوان"},
	"option-n":      {"Option {n}", "Antwort {n}", "الخيار {n}"},
	"task-n":        {"Task {n}", "Aufgabe {n}", "المهمة {n}"},
	"edit-hint":     {"The wording can be changed until the first vote or tick.", "Der Wortlaut lässt sich bis zur ersten Stimme oder zum ersten Abhaken ändern.", "يمكن تغيير الصياغة حتى أول صوت أو علامة."},
	"standing-read": {"This poll can be read here but not voted on. Move it to the conversation so everyone can vote.", "Diese Umfrage kann hier gelesen, aber nicht beantwortet werden. Verschieben Sie sie in die Unterhaltung, damit alle abstimmen können.", "يمكن قراءة هذا الاستطلاع هنا لكن لا يمكن التصويت عليه. انقله إلى المحادثة ليصوّت الجميع."},
}

func chatcmd002EditText(m Model, key string) string {
	copy := chatcmd002EditCopy[key]
	return chatbug039Text(key, copy[chatbug039LocaleIndex(m.Locale)], copy[0])
}

// chatcmd002CanEdit reports whether the reader is offered "Edit poll" or "Edit
// list" on this card: its author, before anybody has voted or ticked, while it
// is open.
func chatcmd002CanEdit(m Model, view chat.Chatcmd002View) bool {
	return m.Chatcmd002.Mutate != nil && view.CanManage && !view.Card.Interacted && !view.Card.Closed(time.Now())
}

// chatcmd002AddOptionRow is the field under a poll's options where a member
// adds one. Enter adds it, like the Add button.
func chatcmd002AddOptionRow(m Model, post string) ui.Node {
	label := chatcmd002EditText(m, "add-option")
	return html.Div(html.Props{Class: "chatcmd002-add"},
		html.Input(html.Props{Class: "chat-input chatcmd002-add-input", Type: "text", MaxLength: 100, Placeholder: label, AutoComplete: "off", Aria: map[string]string{"label": label},
			Data: map[string]string{"chatcmd002-field": "add", "chatcmd002-post": post, "chatcmd002-enter": "chatcmd002-add-option"}}),
		html.Button(html.Props{Type: "button", Class: "button secondary small", Data: map[string]string{"action": "chatcmd002-add-option", "id": post}, Text: chatcmd002EditText(m, "add")}))
}

// chatcmd002EditForm replaces the card's body while its author rewords it: the
// title and every option or task in a field, Save and Cancel. Escape cancels
// and Enter in a field saves.
func chatcmd002EditForm(m Model, post string, card chat.Chatcmd002Card) ui.Node {
	field := func(name, label, value string, max int) ui.Node {
		return html.Label(html.Props{Class: "chatcmd002-edit-field"},
			html.Span(html.Props{Class: "chatcmd002-edit-label", Text: label}),
			html.Input(html.Props{Class: "chat-input", Type: "text", MaxLength: max, Value: value, AutoComplete: "off", Dir: "auto",
				Data: map[string]string{"chatcmd002-field": name, "chatcmd002-post": post, "chatcmd002-enter": "chatcmd002-edit-save", "chatcmd002-escape": "chatcmd002-edit-cancel"}}))
	}
	titleKey, count := "poll-title", 0
	if card.Todo != nil {
		titleKey = "list-title"
	}
	rows := []ui.Node{field("title", chatcmd002EditText(m, titleKey), card.Title, 240)}
	if card.Poll != nil {
		for i, option := range card.Poll.Options {
			count = i + 1
			rows = append(rows, field("opt:"+strconv.Itoa(i), chatcmd002Fill(chatcmd002EditText(m, "option-n"), "n", m.nz(count)), option.Text, 100))
		}
	}
	if card.Todo != nil {
		for i, item := range card.Todo.Items {
			count = i + 1
			rows = append(rows, field("item:"+strconv.Itoa(i), chatcmd002Fill(chatcmd002EditText(m, "task-n"), "n", m.nz(count)), item.Text, 500))
		}
	}
	rows = append(rows,
		html.P(html.Props{Class: "chatcmd002-note", Text: chatcmd002EditText(m, "edit-hint")}),
		html.Div(html.Props{Class: "chatcmd002-foot"},
			html.Button(html.Props{Type: "button", Class: "button small", Data: map[string]string{"action": "chatcmd002-edit-save", "id": post}, Text: chatcmd002EditText(m, "save")}),
			html.Button(html.Props{Type: "button", Class: "button secondary small", Data: map[string]string{"action": "chatcmd002-edit-cancel", "id": post}, Text: chatcmd003Text(m, "cancel")})))
	return html.Div(html.Props{Class: "chatcmd002-edit", Role: "group", Aria: map[string]string{"label": chatcmd003Text(m, card.Kind)}}, rows...)
}

// chatcmd002EditedCard is the card with the words of an edit form put in: the
// title and each option's or task's text, by position. A field that is absent
// keeps its words. ok is false when a field is empty or the card would not
// stand (a repeated option, text too long), and changed says whether any word
// differs from the card.
func chatcmd002EditedCard(card chat.Chatcmd002Card, values map[string]string) (next chat.Chatcmd002Card, changed, ok bool) {
	b := func(name, old string) (string, bool) {
		value, present := values[name]
		if !present {
			return old, true
		}
		value = strings.TrimSpace(value)
		if value != old {
			changed = true
		}
		return value, value != ""
	}
	next = card
	var good bool
	if next.Title, good = b("title", card.Title); !good {
		return card, false, false
	}
	if card.Poll != nil {
		poll := *card.Poll
		poll.Options = append([]chat.ChannelPollOption(nil), card.Poll.Options...)
		for i := range poll.Options {
			if poll.Options[i].Text, good = b("opt:"+strconv.Itoa(i), poll.Options[i].Text); !good {
				return card, false, false
			}
		}
		next.Poll = &poll
	}
	if card.Todo != nil {
		todo := *card.Todo
		todo.Items = append([]chat.Chatcmd002Task(nil), card.Todo.Items...)
		for i := range todo.Items {
			if todo.Items[i].Text, good = b("item:"+strconv.Itoa(i), todo.Items[i].Text); !good {
				return card, false, false
			}
		}
		next.Todo = &todo
	}
	return next, changed, next.Validate() == nil
}

// chatcmd002ActionLocal runs the card actions that keep state on the page (the
// card being reworded) and hands every other to chatcmd002Action.
func chatcmd002ActionLocal(m Model, local localStore, action, post, extra string) bool {
	switch action {
	case "chatcmd002-edit":
		if view, ok := m.Chatcmd002Views[post]; ok && chatcmd002CanEdit(m, view) {
			local.update(func(u *localUI) { u.cardEdit = post })
		}
		return true
	case "chatcmd002-edit-cancel":
		local.update(func(u *localUI) { u.cardEdit = "" })
		return true
	case "chatcmd002-edit-save":
		// A field left empty or a repeated option keeps the form open with the
		// words as typed. Otherwise it closes whether or not a word changed, and a
		// refusal from the server is said on the card, which keeps its old words.
		if view, ok := m.Chatcmd002Views[post]; ok {
			if _, _, valid := chatcmd002EditedCard(view.Card, chatcmd002FieldValues(post)); !valid {
				return true
			}
		}
		chatcmd002Action(m, action, post, extra)
		local.update(func(u *localUI) { u.cardEdit = "" })
		return true
	}
	return chatcmd002Action(m, action, post, extra)
}

// chatcmd002AddOption sends a new option for a poll the reader may add to, and
// reports whether it did. An empty field sends nothing.
func chatcmd002AddOption(m Model, post string, view chat.Chatcmd002View, revision uint64, text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || m.Chatcmd002.Mutate == nil || !view.CanAddOption {
		return false
	}
	m.Chatcmd002.Mutate(post, revision, chat.Chatcmd002Mutation{Operation: "ADD_OPTION", Text: text})
	return true
}

// chatcmd002SaveEdit sends the reworded card when the reader may reword it and
// a word changed.
func chatcmd002SaveEdit(m Model, post string, view chat.Chatcmd002View, revision uint64, values map[string]string) bool {
	if !chatcmd002CanEdit(m, view) {
		return false
	}
	next, changed, ok := chatcmd002EditedCard(view.Card, values)
	if !ok || !changed {
		return false
	}
	m.Chatcmd002.Mutate(post, revision, chat.Chatcmd002Mutation{Operation: "EDIT", Card: &next})
	return true
}

// chatcmd002EditStyles lays out the add-option row and the edit form inside the
// card: one field per line, the add row as a field and a button side by side.
const chatcmd002EditStyles = `.chat-workspace .chatcmd002-add{display:flex;align-items:center;gap:8px;min-width:0}.chat-workspace .chatcmd002-add-input{flex:1 1 auto;min-width:0}.chat-workspace .chatcmd002-add .button{flex:none;width:auto}` +
	`.chat-workspace .chatcmd002-edit{display:grid;gap:8px;min-width:0}.chat-workspace .chatcmd002-edit-field{display:grid;gap:2px;min-width:0}.chat-workspace .chatcmd002-edit-label{color:var(--muted);font-size:.75rem;font-weight:600}` +
	`.chat-workspace .chatcmd002-edit-field .chat-input{box-sizing:border-box;width:100%;min-width:0}` +
	`.chat-workspace .channel-tray-card .channel-poll-option.readonly{cursor:default}.chat-workspace .channel-tray-card .channel-poll-option.readonly:hover{border-color:var(--line)}.chat-workspace .channel-poll-actions{gap:8px}`
