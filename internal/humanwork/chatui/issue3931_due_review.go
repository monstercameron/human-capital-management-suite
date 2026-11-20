package chatui

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// A date or an owner the to-do preview took out of a task's words is a reading
// the author can correct (#3931). Each task that has one gets a choice under
// the card: keep the reading, pick another reading of an ambiguous phrase, or
// have no deadline (no owner) and leave the words in the task. A choice is the
// setting "due-N" or "owner-N", N being the task's place in the list.

var chatcmd003Issue3931Copy = map[string][3]string{
	"due-label": {"Deadline for “{task}”", "Frist für „{task}“", "الموعد النهائي لـ «{task}»"},
	"due-infer": {"Due {date}, read from “{phrase}”", "Fällig am {date}, gelesen aus „{phrase}“", "الموعد {date}، مأخوذ من «{phrase}»"},
	"due-today": {"Today, {date}", "Heute, {date}", "اليوم، {date}"},
	"due-pick":  {"Due {date}", "Fällig am {date}", "الموعد {date}"},
	"due-none":  {"No deadline, keep “{phrase}” in the task", "Keine Frist, „{phrase}“ im Text der Aufgabe lassen", "بلا موعد نهائي، مع إبقاء «{phrase}» في المهمة"},
	"due-ask": {"“{phrase}” can mean the coming day or the same day a week later, so no deadline is set yet. Choose a date below, or keep no deadline.",
		"„{phrase}“ kann den nächsten Tag oder denselben Tag eine Woche später meinen, deshalb ist noch keine Frist gesetzt. Wählen Sie unten ein Datum oder lassen Sie es ohne Frist.",
		"قد تعني «{phrase}» اليوم القادم أو اليوم نفسه بعد أسبوع، لذا لم يُحدَّد موعد نهائي بعد. اختر تاريخًا أدناه أو اترك المهمة بلا موعد."},
	"owner-label": {"Owner for “{task}”", "Zuständig für „{task}“", "المسؤول عن «{task}»"},
	"owner-keep":  {"Assigned to {name}", "Zugewiesen an {name}", "مُسندة إلى {name}"},
	"owner-none":  {"No owner, keep the name in the task", "Keine zuständige Person, Namen im Text der Aufgabe lassen", "بلا مسؤول، مع إبقاء الاسم في المهمة"},
}

func init() {
	for key, value := range chatcmd003Issue3931Copy {
		chatcmd003Copy[key] = value
	}
}

// chatcmd003Review is what the person chose in the due and owner choices of
// the preview, as the reading of the text takes it.
func chatcmd003Review(settings map[string]string) chat.Chatcmd004Review {
	review := chat.Chatcmd004Review{}
	for name, value := range settings {
		switch {
		case strings.HasPrefix(name, "due-"):
			if n, err := strconv.Atoi(strings.TrimPrefix(name, "due-")); err == nil {
				if review.Due == nil {
					review.Due = map[int]string{}
				}
				review.Due[n] = value
			}
		case strings.HasPrefix(name, "owner-"):
			if n, err := strconv.Atoi(strings.TrimPrefix(name, "owner-")); err == nil && value == "none" {
				if review.KeepOwner == nil {
					review.KeepOwner = map[int]bool{}
				}
				review.KeepOwner[n] = true
			}
		}
	}
	return review
}

// chatcmd003ReviewGroup is a radio group of the preview's reviews: one button
// for each option, the chosen one marked, one Tab stop, arrow keys inside.
func chatcmd003ReviewGroup(m Model, state chatcmd003Preview, group, label string, chosen string, keys []string, texts []string) ui.Node {
	labelID := "chatcmd003-" + group + "-label"
	buttons := make([]ui.Node, 0, len(keys))
	stop := chosen
	if !slices.Contains(keys, chosen) && len(keys) > 0 {
		// Nothing chosen yet: the group is still one Tab stop, at its first option.
		stop = keys[0]
	}
	for i, key := range keys {
		class, tab := "chatcmd003-choice", -1
		if key == stop {
			tab = 0
		}
		if key == chosen {
			class, tab = class+" selected", 0
		}
		buttons = append(buttons, html.Button(html.Props{ID: chatcmd003RadioID(group, key), Class: class, Type: "button", Role: "radio", Disabled: state.Busy, TabIndex: tab, Dir: "auto",
			Data: map[string]string{"action": "chatcmd003-set", "id": group, "extra": key},
			Aria: map[string]string{"checked": boolString(key == chosen)}, Text: texts[i]}))
	}
	return html.Div(html.Props{ID: "chatcmd003-" + group, Class: "chatcmd003-setting", Role: "radiogroup", Aria: map[string]string{"labelledby": labelID}},
		html.Span(html.Props{ID: labelID, Class: "chatcmd003-setting-label", Dir: "auto", Text: label}),
		html.Div(html.Props{Class: "chatcmd003-choices"}, buttons...))
}

func chatcmd003ReviewDate(m Model, at *time.Time) string {
	if at == nil {
		return ""
	}
	return chatcmd002DueText(m, chat.Chatcmd002Task{DueAt: at})
}

// chatcmd003Reviews is the preview's part for the dates and owners it read:
// for a task whose date means two things, a status that no deadline is set
// until one is chosen, and for every one the choice.
func chatcmd003Reviews(m Model, state chatcmd003Preview) []ui.Node {
	var nodes []ui.Node
	for _, read := range state.Draft.DueReads {
		group, task := "due-"+strconv.Itoa(read.Item), excerpt(read.Task, 60)
		chosen := state.Settings[group]
		keys, texts, known := make([]string, 0, len(read.Options)), make([]string, 0, len(read.Options)), false
		for _, option := range read.Options {
			known = known || option.Key == chosen
			keys = append(keys, option.Key)
			date := chatcmd003ReviewDate(m, option.At)
			switch option.Key {
			case "infer":
				texts = append(texts, chatcmd002Fill(chatcmd003Text(m, "due-infer"), "date", date, "phrase", read.Phrase))
			case "today":
				texts = append(texts, chatcmd002Fill(chatcmd003Text(m, "due-today"), "date", date))
			case "none":
				texts = append(texts, chatcmd002Fill(chatcmd003Text(m, "due-none"), "phrase", read.Phrase))
			default:
				texts = append(texts, chatcmd002Fill(chatcmd003Text(m, "due-pick"), "date", date))
			}
		}
		if !known {
			chosen = read.Default
		}
		if read.Default == "none" && chosen == "none" {
			nodes = append(nodes, html.P(html.Props{Class: "chatcmd003-note", Role: "status", Dir: "auto", Text: chatcmd002Fill(chatcmd003Text(m, "due-ask"), "phrase", read.Phrase)}))
		}
		nodes = append(nodes, chatcmd003ReviewGroup(m, state, group, chatcmd002Fill(chatcmd003Text(m, "due-label"), "task", task), chosen, keys, texts))
	}
	for _, read := range state.Draft.OwnerReads {
		group, task := "owner-"+strconv.Itoa(read.Item), excerpt(read.Task, 60)
		chosen := "keep"
		if read.Kept {
			chosen = "none"
		}
		nodes = append(nodes, chatcmd003ReviewGroup(m, state, group, chatcmd002Fill(chatcmd003Text(m, "owner-label"), "task", task), chosen,
			[]string{"keep", "none"}, []string{chatcmd002Fill(chatcmd003Text(m, "owner-keep"), "name", read.Name), chatcmd003Text(m, "owner-none")}))
	}
	return nodes
}
