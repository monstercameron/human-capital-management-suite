package chatui

import (
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATSAVE-002. The Saved panel. A saved item is drawn the way the message it
// is is drawn: the avatar and name of its author, then its text through the
// conversation's own body renderer. The two things a person does here are open
// the message where it was said and tick it off; both are one click. The rest
// (a reminder, a note, taking it off the list) is a small icon in the same bar
// the conversation shows when a message is hovered.

// SavedItemKey names one saved message for the panel's own state: the open
// menu, the note being written, the item last used.
func SavedItemKey(host, conversationID, postID string) string {
	return host + "|" + conversationID + "|" + postID
}

func chatsave002RowKey(row SavedMessageRow) string {
	return SavedItemKey(row.TenantID, row.ConversationID, row.PostID)
}

// chatsave002Model is the model the saved messages are drawn with: the
// conversation's own, in the panel's language, with the document titles and the
// origin the panel was given.
func chatsave002Model(view SavedMessagesView) Model {
	m := view.Model
	m.Locale = view.Locale
	if view.DocPreviews != nil {
		m.DocPreviews = view.DocPreviews
	}
	if view.EmbedOrigin != "" {
		m.EmbedOrigin = view.EmbedOrigin
	}
	return m
}

func chatsave002Message(row SavedMessageRow) Message {
	return Message{ID: row.PostID, AuthorID: row.AuthorID, Author: row.Author, Body: row.Body, Revision: row.Revision, Sequence: row.Sequence, SentAt: row.SentAt, PersonaReferences: row.References}
}

// chatsave002Body draws a saved message's text with the conversation's own
// body renderer: the same reader view of the text, the same Markdown, the same
// mention chips, document and share links, and the same projection of an
// agent's answer, in the same message-body element. It returns the message as
// the conversation would know it (an agent's answer carries its actor).
func chatsave002Body(m Model, msg Message) (ui.Node, Message) {
	msg = agentAnnouncementProjectedIdentity(m, personaTrustedMessage(m, msg))
	var text string
	if msg.PersonaActor != nil && msg.PersonaActor.valid() {
		envelope := parseAgentReplyEnvelope(msg.Body)
		text = agentAnswerPresentBody(readerReplyBody(m, msg, envelope.Body), envelope.Sources)
	} else {
		// A poll or a to-do list is read as its words, never as its data, and an
		// "added people" line as its sentence, never as its marker.
		if line, system := chatux021LineText(m, msg); system {
			text = line
		} else {
			// A search or saved row draws a stored announcement as its sentence,
			// never as the envelope it is kept in (CHATBUG-089).
			text = chatDisplayBody(Chatcmd002PlainBody(ReaderMessageBody(m, msg)))
		}
	}
	m.renderReferences = msg.PersonaReferences
	body := ui.Node(html.Div(chatlangBodyProps(m, msg, html.Props{Class: "message-body", Dir: "auto"}), markdownMessageBody(m, text)...))
	body = agentAnnouncementProjectedBody(m, readerAnnouncementMessage(m, msg), body)
	return body, msg
}

// chatsave002LongText is the first guess at whether a message's text will be
// cut at four lines; the browser corrects it by measuring the drawn text.
func chatsave002LongText(body string) bool {
	return strings.Count(body, "\n") >= 4 || len([]rune(body)) > 220
}

func chatsave002Action(row SavedMessageRow, action, label string, extra map[string]string, children ...ui.Node) ui.Node {
	data := map[string]string{"saved-action": action, "saved-host": row.TenantID, "saved-conversation": row.ConversationID, "saved-post": row.PostID}
	aria := map[string]string{"label": label}
	for k, v := range extra {
		if strings.HasPrefix(k, "aria-") {
			aria[strings.TrimPrefix(k, "aria-")] = v
		} else {
			data[k] = v
		}
	}
	return html.Button(html.Props{ID: "chatsave-" + action + "-" + row.PostID, Class: "message-action chatsave-act chatsave-act-" + action, Type: "button", Title: label, Data: data, Aria: aria}, children...)
}

// chatsave002Order puts a reminder that has passed first, the earliest first;
// every other item keeps the order the list came in.
func chatsave002Order(rows []SavedMessageRow, now time.Time) []SavedMessageRow {
	out := make([]SavedMessageRow, 0, len(rows))
	overdue := []SavedMessageRow{}
	for _, row := range rows {
		if !row.Done && SavedReminderOverdue(row.DueAt, now) {
			overdue = append(overdue, row)
			continue
		}
		out = append(out, row)
	}
	for i := 1; i < len(overdue); i++ {
		for j := i; j > 0 && overdue[j].DueAt.Before(overdue[j-1].DueAt); j-- {
			overdue[j], overdue[j-1] = overdue[j-1], overdue[j]
		}
	}
	return append(overdue, out...)
}

func chatsave002Item(view SavedMessagesView, m Model, copy SavedCopy, now time.Time, row SavedMessageRow) ui.Node {
	loc := view.Locale
	key := chatsave002RowKey(row)
	data := map[string]string{"saved-item": "true", "saved-host": row.TenantID, "saved-conversation": row.ConversationID, "saved-post": row.PostID, "saved-sequence": strconv.FormatUint(row.Sequence, 10), "saved-key": key}
	class := "chatsave-item"
	if row.Done {
		class += " is-done"
	}
	if view.Active == key || view.ReminderMenu == key {
		class += " is-active"
	}
	if view.ReminderMenu == key {
		class += " has-menu"
	}
	props := html.Props{ID: "chatsave-item-" + row.PostID, Class: class, Data: data, TabIndex: html.TabIndexZero}

	if row.Availability != "readable" {
		status := copy.NoAccess
		if row.Availability == "removed" {
			status = copy.Removed
		}
		if row.Availability == "deleted" {
			status = copy.Deleted
		}
		props.Class += " is-unavailable"
		return html.Li(props,
			html.Span(html.Props{Class: "chatsave-avatar chatsave-avatar-empty", Aria: map[string]string{"hidden": "true"}}, chatsave002Icon("bookmark", false)),
			html.Div(html.Props{Class: "chatsave-main"}, html.P(html.Props{Class: "chatsave-unavailable", Text: status})),
			html.Div(html.Props{Class: "chatsave-actions", Role: "toolbar", Aria: map[string]string{"label": chatsave002Text(loc, "actions")}},
				chatsave002Action(row, "remove", copy.Unsave, nil, chatsave002Icon("bookmark", true))))
	}

	props.Data["saved-action"] = "open"
	props.Title = chatsave002Text(loc, "item_open")
	body, msg := chatsave002Body(m, chatsave002Message(row))
	author := msg.Author
	if author == "" {
		author = copy.AuthorUnavailable
	}
	lead := ui.Node(personAvatar(m, msg.AuthorID, author, "avatar small chatsave-avatar"))
	meta := []ui.Node{html.Strong(html.Props{Class: "chatsave-author", Dir: "auto", Text: author})}
	if msg.PersonaActor != nil && msg.PersonaActor.valid() {
		lead = integrate1MessageAvatar(m, msg, "avatar small chatsave-avatar")
		meta = append(meta, chatux003MessageBadge(m, msg))
	}
	if row.Channel != "" {
		where := row.Channel
		if row.InChannel {
			where = "#" + strings.TrimPrefix(where, "#")
		}
		meta = append(meta, html.A(html.Props{Class: "chatsave-where", Href: ChannelReferenceURL(row.ConversationID), Dir: "auto", Data: map[string]string{"saved-action": "open-channel", "saved-conversation": row.ConversationID}, Text: chatsave002Text(loc, "in", "where", where)}))
	}
	label := row.TimeLabel
	if !row.SentAt.IsZero() {
		label = SavedTimeLabel(loc, row.SentAt, now)
	}
	if label != "" {
		meta = append(meta, html.Time(html.Props{Class: "chatsave-time", Text: label}))
	}

	expanded := view.Expanded[row.PostID]
	textClass := "chatsave-text"
	if expanded {
		textClass += " is-expanded"
	}
	more := chatsave002Text(loc, "show_more")
	if expanded {
		more = chatsave002Text(loc, "show_less")
	}
	main := []ui.Node{
		html.Div(html.Props{Class: "chatsave-meta"}, meta...),
		html.Div(html.Props{Class: textClass, Data: map[string]string{"saved-text": "true"}}, body),
		html.Button(html.Props{ID: "chatsave-expand-" + row.PostID, Class: "chatsave-more", Type: "button", Hidden: !expanded && !chatsave002LongText(Chatcmd002PlainBody(row.Body)), Text: more, Data: map[string]string{"saved-action": "expand", "saved-post": row.PostID}, Aria: map[string]string{"expanded": boolString(expanded)}}),
	}
	if row.Attachments > 0 {
		main = append(main, html.Div(html.Props{Class: "chatsave-attach"}, chatbug037AttachmentBadge(m, row.Attachments)))
	}

	if row.Done {
		when := SavedDoneLabel(loc, row.DoneAt, now)
		text := copy.Done
		if when != "" {
			text = chatsave002Text(loc, "done_on", "when", when)
		}
		main = append(main, html.Div(html.Props{Class: "chatsave-donebar"},
			html.Span(html.Props{Class: "chatsave-done-mark"}, icon("check"), html.Span(html.Props{Text: text})),
			html.Button(html.Props{ID: "chatsave-reopen-" + row.PostID, Class: "chatsave-reopen", Type: "button", Text: copy.Reopen, Data: map[string]string{"saved-action": "reopen", "saved-host": row.TenantID, "saved-conversation": row.ConversationID, "saved-post": row.PostID}})))
	}

	if !row.DueAt.IsZero() && !row.Done {
		overdue := SavedReminderOverdue(row.DueAt, now)
		due := SavedDueLabel(loc, row.DueAt, now)
		text, chipClass := due, "chatsave-chip"
		if overdue {
			text, chipClass = chatsave002Text(loc, "overdue")+" · "+due, chipClass+" is-overdue"
		}
		main = append(main, html.Div(html.Props{Class: "chatsave-chips"}, html.Span(html.Props{Class: chipClass},
			chatsave002Icon("bell", false),
			html.Button(html.Props{ID: "chatsave-chip-" + row.PostID, Class: "chatsave-chip-label", Type: "button", Data: map[string]string{"saved-action": "remind", "saved-host": row.TenantID, "saved-conversation": row.ConversationID, "saved-post": row.PostID}, Aria: map[string]string{"label": chatsave002Text(loc, "reminder", "when", text), "haspopup": "menu"}, Text: text}),
			html.Button(html.Props{ID: "chatsave-clear-due-" + row.PostID, Class: "chatsave-chip-clear", Type: "button", Title: copy.ClearDue, Data: map[string]string{"saved-action": "clear-due", "saved-host": row.TenantID, "saved-conversation": row.ConversationID, "saved-post": row.PostID}, Aria: map[string]string{"label": copy.ClearDue}}, icon("close")))))
	}

	noteID := "chatsave-note-field-" + row.PostID
	if view.EditingNote == key {
		noteData := map[string]string{"saved-form": "note", "saved-host": row.TenantID, "saved-conversation": row.ConversationID, "saved-post": row.PostID}
		main = append(main, html.Form(html.Props{Class: "chatsave-note-form", Data: noteData},
			html.Label(html.Props{Class: "sr-only", For: noteID, Text: copy.Note}),
			html.Input(html.Props{ID: noteID, Class: "chatsave-note-input", Name: "note", Type: "text", Placeholder: chatsave002Text(loc, "note_ph"), AutoComplete: "off", MaxLength: 4000, Data: map[string]string{"chat-value": row.Note}, Aria: map[string]string{"describedby": "chatsave-status"}}),
			html.P(html.Props{Class: "chatsave-note-hint", Text: chatsave002Text(loc, "note_hint")})))
	} else if strings.TrimSpace(row.Note) != "" {
		main = append(main, html.Button(html.Props{ID: "chatsave-note-" + row.PostID + "-text", Class: "chatsave-note", Type: "button", Title: chatsave002Text(loc, "act_note_edit"), Data: map[string]string{"saved-action": "note", "saved-host": row.TenantID, "saved-conversation": row.ConversationID, "saved-post": row.PostID}, Aria: map[string]string{"label": chatsave002Text(loc, "act_note_edit") + ": " + row.Note}},
			icon("edit"), html.Span(html.Props{Dir: "auto", Text: row.Note})))
	}

	children := []ui.Node{lead, html.Div(html.Props{Class: "chatsave-main"}, main...)}
	if !row.Done {
		hasNote := strings.TrimSpace(row.Note) != ""
		noteLabel := chatsave002Text(loc, "act_note")
		if hasNote {
			noteLabel = chatsave002Text(loc, "act_note_edit")
		}
		// The menu is the bell's own child, so it opens under the bell and the
		// browser measures it against the bell, not against the page.
		anchor := []ui.Node{chatsave002Action(row, "remind", chatsave002Text(loc, "act_remind"), map[string]string{"aria-haspopup": "menu", "aria-expanded": boolString(view.ReminderMenu == key)}, chatsave002Icon("bell", false))}
		if view.ReminderMenu == key {
			anchor = append(anchor, chatsave002Menu(view, row, now))
		}
		children = append(children, html.Div(html.Props{Class: "chatsave-actions", Role: "toolbar", Aria: map[string]string{"label": chatsave002Text(loc, "actions")}},
			chatsave002Action(row, "done", chatsave002Text(loc, "act_done"), nil, icon("check")),
			html.Span(html.Props{Class: "chatsave-anchor"}, anchor...),
			chatsave002Action(row, "note", noteLabel, nil, icon("edit")),
			chatsave002Action(row, "remove", copy.Unsave, nil, chatsave002Icon("bookmark", true))))
	}
	return html.Li(props, children...)
}

// chatsave002Menu is the small menu the bell opens, under the bell.
func chatsave002Menu(view SavedMessagesView, row SavedMessageRow, now time.Time) ui.Node {
	loc := view.Locale
	items := []ui.Node{}
	for _, option := range SavedReminderOptions(loc, now) {
		data := map[string]string{"saved-host": row.TenantID, "saved-conversation": row.ConversationID, "saved-post": row.PostID}
		action := "preset"
		data["saved-preset"] = option.Key
		if option.Key == "pick" {
			action = "remind-pick"
		}
		data["saved-action"] = action
		items = append(items, html.Button(html.Props{ID: "chatsave-" + action + "-" + option.Key + "-" + row.PostID, Class: "menu-item chatsave-menu-item", Type: "button", Role: "menuitem", Text: option.Label, Data: data}))
	}
	if view.PickingDate {
		due := map[string]string{"saved-form": "due", "saved-host": row.TenantID, "saved-conversation": row.ConversationID, "saved-post": row.PostID}
		items = append(items, html.Form(html.Props{Class: "chatsave-pick", Data: due},
			html.Label(html.Props{Class: "sr-only", For: "chatsave-due-" + row.PostID, Text: chatsave002Text(loc, "p_pick")}),
			html.Input(html.Props{ID: "chatsave-due-" + row.PostID, Class: "chatsave-due-input", Name: "due", Type: "datetime-local", Min: now.Format("2006-01-02T15:04"), Required: true, Aria: map[string]string{"describedby": "chatsave-status"}}),
			html.Button(html.Props{Class: "button small", Type: "submit", Text: chatsave002Text(loc, "p_set")})))
	}
	return html.Div(html.Props{ID: "chatsave-menu-" + row.PostID, Class: "chatsave-menu", Role: "menu", Data: map[string]string{"saved-menu": "true"}, Aria: map[string]string{"label": chatsave002Text(loc, "menu_label")}}, items...)
}

func chatsaveSkeleton() ui.Node {
	rows := []ui.Node{}
	for i := 0; i < 3; i++ {
		rows = append(rows, html.Li(html.Props{Class: "chatsave-item chatsave-skeleton", Aria: map[string]string{"hidden": "true"}},
			html.Span(html.Props{Class: "chatsave-avatar"}),
			html.Div(html.Props{Class: "chatsave-main"},
				html.Span(html.Props{Class: "chatsave-bar chatsave-bar-name"}),
				html.Span(html.Props{Class: "chatsave-bar"}),
				html.Span(html.Props{Class: "chatsave-bar chatsave-bar-short"}))))
	}
	return html.Ul(html.Props{Class: "chatsave-items"}, rows...)
}

func chatsave002Button(class, label, action string) ui.Node {
	return html.Button(html.Props{Class: class, Type: "button", Text: label, Data: map[string]string{"saved-action": action}})
}

func chatsave002Render(view SavedMessagesView) ui.Node {
	copy := SavedMessagesCopy(view.Locale)
	loc := view.Locale
	tab := view.Tab
	if tab == "" {
		tab = "todo"
	}
	dir := "ltr"
	if strings.HasPrefix(view.Locale, "ar") {
		dir = "rtl"
	}
	now := view.Now
	if now.IsZero() {
		now = time.Now()
	}
	todo, done, all := view.TodoCount, view.DoneCount, view.AllCount
	if all == 0 && len(view.Rows) > 0 {
		for _, row := range view.Rows {
			switch {
			case row.Availability == "deleted":
			case row.Done:
				done++
			default:
				todo++
			}
		}
		all = len(view.Rows)
	}
	m := chatsave002Model(view)

	segments := []ui.Node{}
	for _, entry := range []struct {
		key, label string
		count      int
	}{{"todo", copy.Todo, todo}, {"done", copy.Done, done}, {"all", copy.All, all}} {
		selected := tab == entry.key
		segment := html.Props{ID: "chatsave-tab-" + entry.key, Class: "chatsave-seg-button", Type: "button", Role: "tab", TabIndex: -1, Data: map[string]string{"saved-tab": entry.key}, Aria: map[string]string{"selected": boolString(selected), "controls": "chatsave-items"}}
		if selected {
			segment.TabIndex = html.TabIndexZero
		}
		segments = append(segments, html.Button(segment, html.Span(html.Props{Text: entry.label}), html.Span(html.Props{Class: "chatsave-seg-count", Text: chatsave002Num(loc, entry.count)})))
	}
	top := []ui.Node{
		// The panel's heading is the one the thread and details panels use: the
		// title and the close icon, in a heading row of the same height. On a
		// phone the panel is a page, and the same close control is its Back.
		// CHATUX-032: the shared panel header, the count on the title's line.
		chatux032Header(chatux032HeaderProps{Class: "chatsave-header", Title: copy.Saved, Subtitle: SavedTodoCountText(loc, todo), SubtitleData: map[string]string{"saved-todo-count": "true"},
			Leading: []ui.Node{html.Button(html.Props{Class: "icon-button chatsave-back", Type: "button", Title: chatsave002Text(loc, "back"), Data: map[string]string{"saved-action": "close"}, Aria: map[string]string{"label": chatsave002Text(loc, "back")}}, icon("arrow-left"))},
			Close:   chatux032CloseButton(copy.Close, false, map[string]string{"saved-action": "close"}, "")}),
		html.Div(html.Props{Class: "chatsave-seg", Role: "tablist", Aria: map[string]string{"label": chatsave002Text(loc, "segments")}}, segments...),
	}
	// The search appears once the list is long enough to need it; it never
	// vanishes while it holds a search.
	if all > 8 || strings.TrimSpace(view.Query) != "" {
		top = append(top, html.Form(html.Props{Class: "chatsave-search", Data: map[string]string{"saved-search": "true"}},
			html.Div(html.Props{Class: "member-filter"},
				html.Label(html.Props{Class: "sr-only", For: "chatsave-search", Text: copy.Search}),
				icon("search"),
				html.Input(html.Props{ID: "chatsave-search", Class: "chat-search", Name: "query", Type: "search", Placeholder: copy.Search, AutoComplete: "off", Data: map[string]string{"chat-value": view.Query}}))))
	}
	nodes := []ui.Node{html.Div(html.Props{Class: "chatsave-top"}, top...)}

	status, statusClass := "", "chatsave-status"
	if view.Loading {
		status, statusClass = copy.Loading, "chatsave-status sr-only"
	}
	if view.Error != "" {
		status = copy.Failed
		if view.Error == "saved_limit" {
			status = copy.Limit
		}
		if view.Error == "invalid_argument" {
			status = copy.Invalid
		}
	}
	if view.ActionError != "" && view.Error == "" {
		status = SavedActionFailedText(view.Locale, view.Action)
	}
	nodes = append(nodes, html.P(html.Props{ID: "chatsave-status", Class: statusClass, Role: "status", Text: status, Aria: map[string]string{"live": "polite"}, Data: map[string]string{"saved-status": "true"}}))
	if view.Error != "" {
		nodes = append(nodes, chatsave002Button("button secondary small chatsave-retry", copy.Retry, "retry"))
	}

	var panel []ui.Node
	switch {
	case view.Loading:
		// CHATUX-037: the shared placeholder; the status line above announces it.
		panel = append(panel, ChatLoadingFrame(LoadingFrame{Locale: loc, Shape: LoadingShapeList, RetryData: map[string]string{"saved-action": "retry"}}))
	case view.Error == "" && len(view.Rows) == 0:
		empty, glyph := copy.EmptyTodo, "bookmark"
		if tab == "done" {
			empty, glyph = copy.EmptyDone, "check"
		}
		if tab == "all" {
			empty = copy.EmptyAll
		}
		// CHATBUG-091: an empty tab says what is true of it and points to the
		// tab that holds items; the first-use sentence is for an empty Saved.
		var emptyLink ui.Node
		if all > 0 {
			empty, emptyLink = chatbug091Empty(loc, tab, todo, done, all)
		}
		if strings.TrimSpace(view.Query) != "" {
			empty, glyph, emptyLink = chatsave002Text(loc, "no_match"), "search", nil
		}
		emptyText := []ui.Node{ui.Text(empty)}
		if emptyLink != nil {
			emptyText = append(emptyText, emptyLink)
		}
		panel = append(panel, html.Div(html.Props{Class: "chatsave-empty"}, chatsave002Icon(glyph, false), html.P(html.Props{}, emptyText...)))
	default:
		items := []ui.Node{}
		for _, row := range chatsave002Order(view.Rows, now) {
			items = append(items, html.WithKey(chatsave002Item(view, m, copy, now, row), row.PostID))
		}
		listClass := "chatsave-items"
		if view.ReminderMenu != "" {
			listClass += " has-menu"
		}
		panel = append(panel, html.Ul(html.Props{Class: listClass}, items...))
	}
	nodes = append(nodes, html.Div(html.Props{ID: "chatsave-items", Class: "chatsave-tabpanel", Role: "tabpanel", Aria: map[string]string{"labelledby": "chatsave-tab-" + tab}}, panel...))
	if view.HasMore {
		nodes = append(nodes, chatsave002Button("button secondary small chatsave-load-more", copy.More, "more"))
	}
	if view.Undo != "" {
		text := chatsave002Text(loc, "u_done")
		if view.Undo == "remove" {
			text = chatsave002Text(loc, "u_remove")
		}
		nodes = append(nodes, html.Div(html.Props{Class: "chatsave-undo", Role: "status", Data: map[string]string{"saved-undo": view.Undo}},
			html.Span(html.Props{Text: text}),
			html.Button(html.Props{ID: "chatsave-undo-button", Class: "chatsave-undo-button", Type: "button", Text: chatsave002Text(loc, "undo"), Data: map[string]string{"saved-action": "undo"}})))
	}
	return html.Div(html.Props{Class: "chatsave-content", Dir: dir, Aria: map[string]string{"busy": boolString(view.Loading)}}, nodes...)
}
