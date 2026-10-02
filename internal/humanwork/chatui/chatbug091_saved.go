package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATBUG-091. Saved used to open on To do however many items sat in Done and
// All, and printed the first-use sentence on a tab with nothing in it while the
// tabs beside it counted items.

// ChatBug091FirstTab is the tab Saved opens on: the first of To do, Done and
// All that holds items; To do when nothing is saved at all.
func ChatBug091FirstTab(todo, done, all int) string {
	switch {
	case todo > 0:
		return "todo"
	case done > 0:
		return "done"
	case all > 0:
		return "all"
	}
	return "todo"
}

var chatbug091Table = map[string][3]string{
	"todo_elsewhere": {"Nothing left to do. Done holds {n}.", "Nichts mehr zu erledigen. Unter Erledigt: {n}.", "لا شيء متبقٍ للتنفيذ. في تم: {n}."},
	"done_elsewhere": {"Nothing is done yet. To do holds {n}.", "Noch nichts erledigt. Unter Zu erledigen: {n}.", "لم يُنجَز شيء بعد. في للتنفيذ: {n}."},
	"none_to_do":     {"Nothing to do and nothing done. All holds {n}.", "Nichts zu erledigen und nichts erledigt. Unter Alle: {n}.", "لا شيء للتنفيذ ولا شيء مُنجز. في الكل: {n}."},
	"show_done":      {"Show Done", "Erledigt anzeigen", "عرض تم"},
	"show_todo":      {"Show To do", "Zu erledigen anzeigen", "عرض للتنفيذ"},
	"show_all":       {"Show All", "Alle anzeigen", "عرض الكل"},
}

func chatbug091Text(locale, key string, pairs ...string) string {
	values, ok := chatbug091Table[key]
	if !ok {
		return ""
	}
	text := values[0]
	switch dateLocale(locale) {
	case "de":
		text = values[1]
	case "ar":
		text = values[2]
	}
	text = chatbug039Text(key, text, values[0])
	for i := 0; i+1 < len(pairs); i += 2 {
		text = strings.ReplaceAll(text, "{"+pairs[i]+"}", pairs[i+1])
	}
	return text
}

// chatbug091Empty says what is true of an empty tab. The first-use sentence is
// for a person who has never saved anything; a tab that is empty while another
// holds items names the other tab and offers it as one press.
func chatbug091Empty(locale, tab string, todo, done, all int) (text string, link ui.Node) {
	copy := SavedMessagesCopy(locale)
	var key, target, label string
	var count int
	switch {
	case all == 0:
		switch tab {
		case "done":
			return copy.EmptyDone, nil
		case "all":
			return copy.EmptyAll, nil
		}
		return copy.EmptyTodo, nil
	case tab == "todo" && done > 0:
		key, target, label, count = "todo_elsewhere", "done", "show_done", done
	case tab == "done" && todo > 0:
		key, target, label, count = "done_elsewhere", "todo", "show_todo", todo
	default:
		key, target, label, count = "none_to_do", "all", "show_all", all
	}
	link = html.Button(html.Props{Class: "chatsave-empty-link", Type: "button", Text: chatbug091Text(locale, label), Data: map[string]string{"saved-tab": target}})
	return chatbug091Text(locale, key, "n", chatsave002Num(locale, count)), link
}
