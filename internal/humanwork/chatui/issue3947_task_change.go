package chatui

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// Issue 3947: the owner of a posted to-do list changes who has one task, or when
// it is due, from the card itself. "Change owner or date" sits beside the task
// the way "Edit list" sits on the card; it opens a small form in the task's row
// with the people of the conversation, a date and an optional time. Save sends
// one CHANGE_TASK for what actually differs and the card shows the answer; the
// task then says who changed it and when. The server decides everything: the
// page offers the form only where the view says the reader manages the card and
// it is open, and a refusal or a stale card is said on the card as for a tick.

const (
	issue3947Edit   = "chatcmd002-task-edit"
	issue3947Save   = "chatcmd002-task-save"
	issue3947Cancel = "chatcmd002-task-cancel"
	issue3947Sep    = "\x1f"
)

// issue3947Fields reads the form's fields from the page; tests replace it.
var issue3947Fields = chatcmd002FieldValues

// issue3947State is the task form a card has open: which task and why the last
// Save sent nothing.
type issue3947State struct {
	Post, Item string
	Note       string // copy key of why the last Save kept the form open
}

var issue3947Copy = map[string][3]string{
	"change":           {"Change owner or date", "Besitzer oder Datum ändern", "تغيير المسؤول أو الموعد"},
	"owner":            {"Assigned to", "Zugewiesen an", "مسندة إلى"},
	"nobody":           {"Nobody", "Niemand", "لا أحد"},
	"date":             {"Due date", "Fälligkeitsdatum", "تاريخ الاستحقاق"},
	"time":             {"Time (optional)", "Uhrzeit (optional)", "الوقت (اختياري)"},
	"hint":             {"Leave the date empty to remove it. A finished task stays finished.", "Lassen Sie das Datum leer, um es zu entfernen. Eine erledigte Aufgabe bleibt erledigt.", "اترك التاريخ فارغًا لإزالته. تبقى المهمة المنجزة منجزة."},
	"hint-assignee":    {"On this list only the person a task is assigned to can tick it, so every task needs someone.", "In dieser Liste kann nur die zugewiesene Person eine Aufgabe abhaken, daher braucht jede Aufgabe jemanden.", "في هذه القائمة لا يضع علامة على المهمة إلا الشخص المسندة إليه، لذا تحتاج كل مهمة إلى مسؤول."},
	"changed-assignee": {"Owner changed by {name}, {date}", "Besitzer geändert von {name}, {date}", "غيّر {name} المسؤول، {date}"},
	"changed-due":      {"Due date changed by {name}, {date}", "Fälligkeit geändert von {name}, {date}", "غيّر {name} الموعد، {date}"},
	"changed-both":     {"Owner and due date changed by {name}, {date}", "Besitzer und Fälligkeit geändert von {name}, {date}", "غيّر {name} المسؤول والموعد، {date}"},
	"note-date":        {"That date and time do not exist in your time zone. Choose another.", "Dieses Datum und diese Uhrzeit gibt es in Ihrer Zeitzone nicht. Wählen Sie ein anderes.", "هذا التاريخ والوقت غير موجودين في منطقتك الزمنية. اختر غيرهما."},
	"note-past":        {"Choose a date and time that is still ahead.", "Wählen Sie ein Datum und eine Uhrzeit, die noch bevorstehen.", "اختر تاريخًا ووقتًا لم يحلّا بعد."},
	"note-owner":       {"Choose who this task is for.", "Wählen Sie, für wen diese Aufgabe ist.", "اختر من تُسند إليه هذه المهمة."},
	"note-time":        {"Choose a date for that time, or clear the time.", "Wählen Sie ein Datum für diese Uhrzeit oder leeren Sie die Uhrzeit.", "اختر تاريخًا لهذا الوقت أو امسح الوقت."},
}

func issue3947Text(m Model, key string) string {
	copy := issue3947Copy[key]
	return chatbug039Text(key, copy[chatbug039LocaleIndex(m.Locale)], copy[0])
}

func issue3947OpenID(post, item string) string  { return "issue3947-open-" + post + "-" + item }
func issue3947FirstID(post, item string) string { return "issue3947-owner-" + post + "-" + item }

// issue3947CanChange reports whether a reader is offered the form on a card:
// they manage it, it is a list, it is open, and the page can send.
func issue3947CanChange(m Model, view chat.Chatcmd002View, preview, closed bool) bool {
	return !preview && !closed && view.CanManage && view.Card.Todo != nil && m.Chatcmd002.Mutate != nil
}

// issue3947Owners is the people a task can be given: the conversation's people,
// not its agents, with the task's present owner kept when they are no longer
// there so that Save does not move the task by accident.
func issue3947Owners(m Model, task chat.Chatcmd002Task) (people []Member, keepFormer bool) {
	present := false
	for _, member := range m.Members {
		if member.Agent || member.ID == "" {
			continue
		}
		people = append(people, member)
		present = present || (task.AssigneeID != "" && member.ID == task.AssigneeID && (member.HomeTenantID == task.AssigneeHomeTenantID || member.HomeTenantID == ""))
	}
	return people, task.AssigneeID != "" && !present
}

func issue3947Home(member Member, fallback string) string {
	if member.HomeTenantID != "" {
		return member.HomeTenantID
	}
	return fallback
}

// issue3947Row is the task's extra line items: the record of the last change
// and the control that opens the form.
func issue3947Row(m Model, post string, view chat.Chatcmd002View, task chat.Chatcmd002Task, preview, closed bool) []ui.Node {
	var parts []ui.Node
	if task.ChangedBySubjectID != "" && task.Changed != "" {
		key := map[string]string{chat.Chatcmd002ChangedAssignee: "changed-assignee", chat.Chatcmd002ChangedDue: "changed-due", chat.Chatcmd002ChangedAssigneeDue: "changed-both"}[task.Changed]
		if key != "" {
			parts = append(parts, html.Span(html.Props{Class: "chatcmd002-by", Dir: "auto", Text: chatcmd002Fill(issue3947Text(m, key), "name", chatcmd002PersonName(m, task.ChangedByHomeTenantID, task.ChangedBySubjectID), "date", chatcmd002Stamp(m, time.Unix(task.ChangedAtUnix, 0)))}))
		}
	}
	if !issue3947CanChange(m, view, preview, closed) {
		return parts
	}
	if m.issue3947.Post == post && m.issue3947.Item == task.ID {
		return append(parts, issue3947Form(m, post, view, task))
	}
	label := issue3947Text(m, "change") + ": " + task.Text
	return append(parts, html.Button(html.Props{Type: "button", ID: issue3947OpenID(post, task.ID), Class: "chatcmd002-link", Title: label, Data: map[string]string{"action": issue3947Edit, "id": post, "extra": task.ID},
		Aria: map[string]string{"label": label}, Text: issue3947Text(m, "change")}))
}

func issue3947Form(m Model, post string, view chat.Chatcmd002View, task chat.Chatcmd002Task) ui.Node {
	enter := map[string]string{"chatcmd002-post": post, "chatcmd002-enter": issue3947Save, "chatcmd002-escape": issue3947Cancel}
	data := func(name, value string) map[string]string {
		out := map[string]string{"chatcmd002-field": name, "chat-value": value}
		for k, v := range enter {
			out[k] = v
		}
		return out
	}
	people, former := issue3947Owners(m, task)
	current := task.AssigneeHomeTenantID + issue3947Sep + task.AssigneeID
	var options []ui.Node
	assigneeOnly := view.Card.Todo.Tick == "assignee"
	if !assigneeOnly {
		options = append(options, html.Option(html.Props{Value: issue3947Sep, Text: issue3947Text(m, "nobody"), Selected: task.AssigneeID == ""}))
	}
	if former {
		options = append(options, html.Option(html.Props{Value: current, Text: chatcmd003Text(m, "former-member"), Selected: true}))
	}
	for _, member := range people {
		value := issue3947Home(member, m.CurrentTenantID) + issue3947Sep + member.ID
		if member.HomeTenantID == "" && task.AssigneeID == member.ID {
			value = current
		}
		name := member.Name
		if member.ID == m.CurrentUser {
			name = m.t(KeyYou)
		}
		options = append(options, html.Option(html.Props{Value: value, Text: name, Selected: !former && value == current}))
	}
	selected := issue3947Sep
	if task.AssigneeID != "" {
		selected = current
	}
	selectData := data("task-owner", selected)
	selectData["chat-select-value"] = selected
	selectData["chat-select-version"] = post + ":" + task.ID + ":" + strconv.FormatUint(view.Revision, 10)
	selectData["chat-select-editable"] = "true"
	loc := chatcmd003Location(m)
	date, clock := "", ""
	if task.DueAt != nil {
		date = task.DueAt.In(loc).Format("2006-01-02")
		if task.DueTimed {
			at := task.DueAt.In(loc)
			clock = twoDigits(at.Hour()) + ":" + twoDigits(at.Minute())
		}
	}
	field := func(label string, control ui.Node) ui.Node {
		return html.Label(html.Props{Class: "chatcmd002-edit-field"}, html.Span(html.Props{Class: "chatcmd002-edit-label", Text: label}), control)
	}
	hint := "hint"
	if assigneeOnly {
		hint = "hint-assignee"
	}
	rows := []ui.Node{
		field(issue3947Text(m, "owner"), html.Select(html.Props{ID: issue3947FirstID(post, task.ID), Class: "chat-input", Data: selectData, Dir: "auto"}, options...)),
		field(issue3947Text(m, "date"), html.Input(html.Props{Class: "chat-input", Type: "date", Data: data("task-date", date)})),
		field(issue3947Text(m, "time"), html.Input(html.Props{Class: "chat-input", Type: "time", Data: data("task-time", clock)})),
		html.P(html.Props{Class: "chatcmd002-note", Text: issue3947Text(m, hint)}),
	}
	if m.issue3947.Note != "" {
		rows = append(rows, html.P(html.Props{Class: "chatcmd002-note", Role: "alert", Text: issue3947Text(m, m.issue3947.Note)}))
	}
	rows = append(rows, html.Div(html.Props{Class: "chatcmd002-foot"},
		html.Button(html.Props{Type: "button", Class: "button small", Data: map[string]string{"action": issue3947Save, "id": post}, Text: chatcmd002EditText(m, "save")}),
		html.Button(html.Props{Type: "button", Class: "button secondary small", Data: map[string]string{"action": issue3947Cancel, "id": post}, Text: chatcmd003Text(m, "cancel")})))
	return html.Div(html.Props{Class: "chatcmd002-edit chatcmd002-task-form", Role: "group", Aria: map[string]string{"label": issue3947Text(m, "change") + ": " + task.Text}}, rows...)
}

// issue3947Change is the mutation Save sends for the form's values, "" as the
// note when there is one, and whether the form may close. Only what differs from
// the task is sent: an owner that is the same person, a date that is the same
// moment, send nothing.
func issue3947Change(m Model, view chat.Chatcmd002View, item string, values map[string]string) (mutation chat.Chatcmd002Mutation, note string, send bool) {
	if view.Card.Todo == nil {
		return mutation, "", false
	}
	var task *chat.Chatcmd002Task
	for i := range view.Card.Todo.Items {
		if view.Card.Todo.Items[i].ID == item {
			task = &view.Card.Todo.Items[i]
		}
	}
	if task == nil {
		return mutation, "", false
	}
	mutation = chat.Chatcmd002Mutation{Operation: chat.Chatcmd002OperationChangeTask, ItemID: item}
	if owner, present := values["task-owner"]; present {
		home, person, _ := strings.Cut(owner, issue3947Sep)
		if person != task.AssigneeID || home != task.AssigneeHomeTenantID {
			if person == "" && view.Card.Todo.Tick == "assignee" {
				return mutation, "note-owner", false
			}
			mutation.SetAssignee, mutation.AssigneeHomeTenantID, mutation.AssigneeID = true, home, person
		}
	}
	date, clock := strings.TrimSpace(values["task-date"]), strings.TrimSpace(values["task-time"])
	_, hasDate := values["task-date"]
	if hasDate {
		switch {
		case date == "" && clock != "":
			return mutation, "note-time", false
		case date == "":
			if task.DueAt != nil {
				mutation.SetDue = true
			}
		default:
			timed := clock != ""
			if issue3947SameDue(m, task, date, clock) {
				// The date the task already has is not a new deadline: a late task can
				// still be handed on without moving its date.
				break
			}
			at, err := chat.Chatcmd004ResolveDateTime(date, orClock(clock), chatcmd003Location(m), time.Now())
			switch {
			case errors.Is(err, chat.ErrDeadlinePast):
				return mutation, "note-past", false
			case err != nil:
				return mutation, "note-date", false
			}
			if task.DueAt == nil || !task.DueAt.Equal(at) || task.DueTimed != timed {
				mutation.SetDue, mutation.DueAt, mutation.DueTimed = true, &at, timed
			}
		}
	}
	return mutation, "", mutation.SetAssignee || mutation.SetDue
}

// issue3947SameDue reports whether the form's date and time read as the task's
// present deadline in the reader's time zone, as the form showed them.
func issue3947SameDue(m Model, task *chat.Chatcmd002Task, date, clock string) bool {
	if task.DueAt == nil {
		return false
	}
	at := task.DueAt.In(chatcmd003Location(m))
	if at.Format("2006-01-02") != date {
		return false
	}
	if !task.DueTimed {
		return clock == ""
	}
	return clock == twoDigits(at.Hour())+":"+twoDigits(at.Minute())
}

// orClock is the time of day a date alone stands for: the end of that day, as a
// date typed in the composer does.
func orClock(clock string) string {
	if clock == "" {
		return "23:59"
	}
	return clock
}

// issue3947Action runs the task form's presses: open it, cancel it, save it.
// It returns false for any other action.
func issue3947Action(m Model, local localStore, action, post, extra string) bool {
	switch action {
	case issue3947Edit, issue3947Save, issue3947Cancel:
	default:
		return false
	}
	view, loaded := m.Chatcmd002Views[post]
	if action == issue3947Cancel {
		state := local.get().task
		local.update(func(u *localUI) { u.task = issue3947State{} })
		if state.Post == post && state.Item != "" {
			focusChatLayerField(issue3947OpenID(post, state.Item))
		}
		return true
	}
	if !loaded || !issue3947CanChange(m, view, false, view.Card.Closed(time.Now())) {
		local.update(func(u *localUI) {
			if u.task.Post == post {
				u.task = issue3947State{}
			}
		})
		return true
	}
	if action == issue3947Edit {
		exists := false
		for _, task := range view.Card.Todo.Items {
			exists = exists || task.ID == extra
		}
		if exists {
			local.update(func(u *localUI) { u.task = issue3947State{Post: post, Item: extra} })
			focusChatLayerField(issue3947FirstID(post, extra))
		}
		return true
	}
	state := local.get().task
	if state.Post != post || state.Item == "" {
		return true
	}
	mutation, note, send := issue3947Change(m, view, state.Item, issue3947Fields(post))
	if note != "" {
		local.update(func(u *localUI) { u.task.Note = note })
		return true
	}
	if send {
		m.Chatcmd002.Mutate(post, chatcmd002Revision(m, post, view), mutation)
	}
	local.update(func(u *localUI) { u.task = issue3947State{} })
	focusChatLayerField(issue3947OpenID(post, state.Item))
	return true
}

// chatcmd002Revision is the revision of the message that carries the card, as
// the page last read it: the timeline's, the thread's, or the view's own.
func chatcmd002Revision(m Model, post string, view chat.Chatcmd002View) uint64 {
	revision := view.Revision
	for _, msg := range append(append([]Message{}, m.Messages...), m.ThreadMessages...) {
		if msg.ID == post {
			revision = msg.Revision
		}
	}
	if m.ThreadParent != nil && m.ThreadParent.ID == post {
		revision = m.ThreadParent.Revision
	}
	return revision
}

const issue3947Styles = `.chat-workspace .chatcmd002-task-form{flex:1 1 100%;min-width:0;padding-block:4px}.chat-workspace .chatcmd002-task-form select,.chat-workspace .chatcmd002-task-form input{box-sizing:border-box;width:100%;min-width:0;min-height:36px}` +
	`@media(pointer:coarse){.chat-workspace .chatcmd002-task-form select,.chat-workspace .chatcmd002-task-form input,.chat-workspace .chatcmd002-task-form .button,.chat-workspace .chatcmd002-task .chatcmd002-link{min-height:44px}}`
