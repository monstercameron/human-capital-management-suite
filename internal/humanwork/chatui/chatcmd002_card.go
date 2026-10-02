package chatui

import (
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type Chatcmd002View = chat.Chatcmd002View

// A poll or a to-do list posted as a message is drawn in the conversation
// where it was posted, by this card. The message body carries the card; the
// view (who voted, what the reader may do, the counts the reader may see) is
// read from the server and kept in Model.Chatcmd002Views. Until that view
// arrives the card shows what the body says and takes no presses.

// chatcmd002Fill replaces {name} placeholders in a copy line.
func chatcmd002Fill(text string, values ...string) string {
	for i := 0; i+1 < len(values); i += 2 {
		text = strings.ReplaceAll(text, "{"+values[i]+"}", values[i+1])
	}
	return text
}

// chatcmd002Votes is the line under a poll's question: how many votes it holds.
func chatcmd002Votes(m Model, total int) string {
	switch total {
	case 0:
		return chatcmd003Text(m, "votes-none")
	case 1:
		return chatcmd003Text(m, "vote-one")
	}
	return chatcmd002Fill(chatcmd003Text(m, "votes"), "n", m.nz(total))
}

func chatcmd002Stamp(m Model, at time.Time) string {
	return at.In(chatcmd003Location(m)).Format("2006-01-02 15:04")
}

// chatcmd002Poll draws a poll's options. A named poll votes on the press and
// the press on one's own choice withdraws it. An anonymous ballot is final, so
// it is chosen first and cast with its own button.
func chatcmd002Poll(m Model, post string, view chat.Chatcmd002View, preview, closed bool) []ui.Node {
	poll := view.Card.Poll
	total := 0
	for _, option := range poll.Options {
		total += option.Count
	}
	showResults := preview || view.ResultsVisible
	meta := []string{}
	if showResults {
		meta = append(meta, chatcmd002Votes(m, total))
	}
	if poll.Multiple {
		meta = append(meta, chatcmd003Text(m, "chip-multiple"))
	}
	if poll.Anonymous {
		meta = append(meta, chatcmd003Text(m, "chip-anonymous"))
	}
	switch {
	case view.Card.ClosedAt != nil:
		meta = append(meta, chatcmd002Fill(chatcmd003Text(m, "closed-at"), "date", chatcmd002Stamp(m, *view.Card.ClosedAt)))
	case closed && poll.ClosesAt != nil:
		meta = append(meta, chatcmd002Fill(chatcmd003Text(m, "closed-at"), "date", chatcmd002Stamp(m, *poll.ClosesAt)))
	case poll.ClosesAt != nil:
		meta = append(meta, chatcmd002Fill(chatcmd003Text(m, "closes-at"), "date", chatcmd002Stamp(m, *poll.ClosesAt)))
	}
	nodes := []ui.Node{}
	if len(meta) > 0 {
		nodes = append(nodes, html.P(html.Props{Class: "chatcmd002-meta", Dir: "auto", Text: strings.Join(meta, " · ")}))
	}
	locked := preview || closed || m.Chatcmd002.Mutate == nil
	ballot := poll.Anonymous && !preview
	var rows []ui.Node
	for _, option := range poll.Options {
		mine := false
		for _, id := range view.MyOptions {
			mine = mine || id == option.ID
		}
		result := []ui.Node{}
		if showResults {
			percent := pollPercent(option.Count, total)
			result = append(result,
				html.Span(html.Props{Class: "chatcmd002-count", Text: chatCount(m.Locale, option.Count) + " · " + chatCount(m.Locale, percent) + "%"}),
				html.Progress(html.Props{Class: "chatcmd002-bar", Raw: map[string]any{"value": percent, "max": 100}, Aria: map[string]string{"label": option.Text}}))
		}
		text := html.Span(html.Props{Class: "chatcmd002-option-text", Dir: "auto", Text: option.Text})
		var row []ui.Node
		if ballot {
			// The choice is a native control, read when Cast vote is pressed.
			kind := "radio"
			if poll.Multiple {
				kind = "checkbox"
			}
			id := "chatcmd002-choice-" + post + "-" + option.ID
			row = append(row, html.Label(html.Props{Class: "chatcmd002-choice", For: id},
				append([]ui.Node{html.Input(html.Props{ID: id, Type: kind, Name: "chatcmd002-choice-" + post, Value: option.ID, Disabled: locked || view.Voted, Data: map[string]string{"chatcmd002-choice": post}}), text}, result...)...))
		} else {
			label := chatcmd003Text(m, "vote") + ": " + option.Text
			class := "chatcmd002-vote"
			if mine {
				label = chatcmd003Text(m, "voted") + ": " + option.Text + ". " + chatcmd003Text(m, "withdraw")
				class += " selected"
			}
			row = append(row, html.Button(html.Props{Type: "button", Class: class, Disabled: locked, Title: label,
				Data: map[string]string{"action": "chatcmd002-vote", "id": post, "extra": option.ID},
				Aria: map[string]string{"pressed": boolString(mine), "label": label}}, append([]ui.Node{text}, result...)...))
		}
		if showResults && !poll.Anonymous {
			names := []string{}
			for _, voter := range view.Voters[option.ID] {
				names = append(names, chatcmd002PersonName(m, voter.HomeTenantID, voter.SubjectID))
			}
			if len(names) > 0 {
				if hidden := option.Count - len(names); hidden > 0 {
					names = append(names, "+"+m.nz(hidden))
				}
				row = append(row, html.P(html.Props{Class: "chatcmd002-voters", Dir: "auto", Text: strings.Join(names, ", ")}))
			}
		}
		class := "chatcmd002-option"
		if mine {
			class += " selected"
		}
		rows = append(rows, html.Li(html.Props{Class: class}, row...))
	}
	nodes = append(nodes, html.Ul(html.Props{Class: "chatcmd002-options"}, rows...))
	if !preview && !closed && view.CanAddOption && m.Chatcmd002.Mutate != nil {
		nodes = append(nodes, chatcmd002AddOptionRow(m, post))
	}
	if ballot && !closed {
		if view.Voted {
			nodes = append(nodes, html.P(html.Props{Class: "chatcmd002-note", Role: "status", Text: chatcmd003Text(m, "cast-done")}))
		} else {
			nodes = append(nodes, html.Div(html.Props{Class: "chatcmd002-foot"},
				html.Button(html.Props{Type: "button", Class: "button small", Disabled: locked, Data: map[string]string{"action": "chatcmd002-cast", "id": post}, Text: chatcmd003Text(m, "cast")}),
				html.Span(html.Props{Class: "chatcmd002-note", Text: chatcmd003Text(m, "cast-hint")})))
		}
	}
	if !showResults {
		key := "waiting-results"
		if poll.Results == "after-closing" {
			key = "waiting-closing"
		}
		nodes = append(nodes, html.P(html.Props{Class: "chatcmd002-note", Role: "status", Text: chatcmd003Text(m, key)}))
	}
	return nodes
}

// chatcmd002Todo draws a list's tasks, one line each: the tick, the text, who
// has it and by when, and who finished it.
func chatcmd002Todo(m Model, post string, view chat.Chatcmd002View, preview, closed bool) []ui.Node {
	todo := view.Card.Todo
	done := 0
	for _, item := range todo.Items {
		if item.Completed {
			done++
		}
	}
	nodes := []ui.Node{html.P(html.Props{Class: "chatcmd002-meta", Text: chatcmd002Fill(chatcmd003Text(m, "done-count"), "done", m.nz(done), "total", m.nz(len(todo.Items)))})}
	var rows []ui.Node
	for _, item := range todo.Items {
		label := chatcmd003Text(m, "tick")
		if item.Completed {
			label = chatcmd003Text(m, "untick")
		}
		box := []ui.Node{}
		if item.Completed {
			box = append(box, icon("check"))
		}
		class := "chatcmd002-task"
		if item.Completed {
			class += " done"
		}
		parts := []ui.Node{
			html.Button(html.Props{Type: "button", Role: "checkbox", Class: "chatcmd002-tick", Title: label, Disabled: preview || closed || !view.CanTick[item.ID] || m.Chatcmd002.Mutate == nil,
				Data: map[string]string{"action": "chatcmd002-tick", "id": post, "extra": item.ID},
				Aria: map[string]string{"checked": boolString(item.Completed), "label": label + ": " + item.Text}}, box...),
			html.Span(html.Props{Class: "chatcmd002-task-text", Dir: "auto", Text: item.Text}),
		}
		if item.AssigneeID != "" {
			parts = append(parts, html.Span(html.Props{Class: "chatcmd002-chip", Dir: "auto", Text: chatcmd002Fill(chatcmd003Text(m, "assigned"), "name", chatcmd002PersonName(m, item.AssigneeHomeTenantID, item.AssigneeID))}))
		}
		if item.DueAt != nil {
			parts = append(parts, html.Time(html.Props{Class: "chatcmd002-chip", Text: chatcmd002Fill(chatcmd003Text(m, "due"), "date", item.DueAt.In(chatcmd003Location(m)).Format("2006-01-02"))}))
		}
		if item.Completed {
			name := chatcmd002PersonName(m, item.CompletedByHomeTenantID, item.CompletedBySubjectID)
			when := chatcmd002Stamp(m, time.Unix(item.CompletedAtUnix, 0))
			parts = append(parts, html.Span(html.Props{Class: "chatcmd002-by", Dir: "auto", Title: chatcmd002Fill(chatcmd003Text(m, "completed"), "name", name, "date", when), Text: chatcmd002Fill(chatcmd003Text(m, "completed-by"), "name", name)}))
		}
		if !preview && m.Chatcmd002.AddToTasks != nil {
			parts = append(parts, html.Button(html.Props{Type: "button", Class: "chatcmd002-link", Data: map[string]string{"action": "chatcmd002-add-tasks", "id": post, "extra": item.ID}, Text: chatcmd003Text(m, "add-tasks")}))
		}
		if !preview && m.Chatcmd002.MoveToChannel != nil && !item.Completed {
			parts = append(parts, html.Button(html.Props{Type: "button", Class: "chatcmd002-link", Data: map[string]string{"action": "chatcmd002-move-channel", "id": post, "extra": item.ID}, Text: chatcmd003Text(m, "move-channel")}))
		}
		rows = append(rows, html.Li(html.Props{Class: class}, parts...))
	}
	return append(nodes, html.Ul(html.Props{Class: "chatcmd002-tasks"}, rows...))
}

func Chatcmd002RenderCard(m Model, post string, view chat.Chatcmd002View, preview bool) ui.Node {
	c := view.Card
	closed := c.Closed(time.Now())
	glyph := "poll"
	if c.Todo != nil {
		glyph = "checklist"
	}
	head := []ui.Node{icon(glyph), html.H4(html.Props{Class: "chatcmd002-title", Dir: "auto", Text: c.Title})}
	if closed {
		head = append(head, html.Span(html.Props{Class: "chatcmd002-closed", Text: chatcmd003Text(m, "closed")}))
	}
	children := []ui.Node{html.Div(html.Props{Class: "chatcmd002-head"}, head...)}
	editing := !preview && post != "" && m.cardEditing == post && chatcmd002CanEdit(m, view)
	switch {
	case editing:
		children = append(children, chatcmd002EditForm(m, post, c))
	case c.Poll != nil:
		children = append(children, chatcmd002Poll(m, post, view, preview, closed)...)
	case c.Todo != nil:
		children = append(children, chatcmd002Todo(m, post, view, preview, closed)...)
	}
	if view.Notice != "" && !preview {
		children = append(children, html.P(html.Props{Class: "chatcmd002-note", Role: "alert", Text: chatcmd003Text(m, "card-"+view.Notice)}))
	}
	if !preview && view.CanManage && !editing {
		label, operation := "close-poll", "CLOSE"
		editLabel := "edit-poll"
		if c.Todo != nil {
			label, editLabel = "close-list", "edit-list"
		}
		if closed {
			label, operation = "reopen-poll", "REOPEN"
			if c.Todo != nil {
				label = "reopen-list"
			}
		}
		foot := []ui.Node{}
		if chatcmd002CanEdit(m, view) {
			// Wording can change until the first vote or tick.
			foot = append(foot, html.Button(html.Props{Class: "chatcmd002-link", Type: "button", Data: map[string]string{"action": "chatcmd002-edit", "id": post}, Text: chatcmd002EditText(m, editLabel)}))
		}
		foot = append(foot, html.Button(html.Props{Class: "chatcmd002-link", Type: "button", Disabled: m.Chatcmd002.Mutate == nil, Data: map[string]string{"action": "chatcmd002-close", "id": post, "extra": operation}, Text: chatcmd003Text(m, label)}))
		children = append(children, html.Div(html.Props{Class: "chatcmd002-foot"}, foot...))
	}
	return html.Section(html.Props{Class: "chatcmd002-card", Dir: agentReplyDirection(m.Locale), Data: map[string]string{"card": c.Kind}, Aria: map[string]string{"label": chatcmd003Text(m, c.Kind)}}, children...)
}

func chatcmd002ProjectedBody(m Model, msg Message, fallback ui.Node) ui.Node {
	c, ok := chat.Chatcmd002Decode(msg.Body)
	if !ok {
		return fallback
	}
	view, loaded := m.Chatcmd002Views[msg.ID]
	if !loaded {
		// Only what the body says: no counts it was not allowed to show, and
		// nothing to press until the server has said what this reader may do.
		view = chat.Chatcmd002View{Card: c, ResultsVisible: c.Poll != nil && c.Poll.Results == "always"}
		m.Chatcmd002.Mutate = nil
	}
	return Chatcmd002RenderCard(m, msg.ID, view, false)
}

// Chatcmd002PlainBody is a message body as text a person reads: a card's
// title and lines, without the data that follows them. Any surface that shows
// a body as text (a search result, a saved item, a copied message) uses this,
// or it prints the card's data.
func Chatcmd002PlainBody(body string) string {
	if text, _, found := strings.Cut(body, chat.Chatcmd002BodyMarker); found {
		if _, ok := chat.Chatcmd002Decode(body); ok {
			return text
		}
	}
	return body
}

// chatcmd002IsCard reports whether a message is a poll or a to-do list. Such a
// message changes with every vote and tick, which is not an edit of its text.
func chatcmd002IsCard(body string) bool {
	_, ok := chat.Chatcmd002Decode(body)
	return ok
}

// chatcmd002NextVote is the ballot after one option of a named poll is
// pressed. With several choices the press toggles that option; with one choice
// it becomes the choice, and pressing the current choice withdraws it.
func chatcmd002NextVote(poll chat.Chatcmd002Poll, mine []string, pressed string) []string {
	had := false
	var rest []string
	for _, id := range mine {
		if id == pressed {
			had = true
		} else {
			rest = append(rest, id)
		}
	}
	switch {
	case poll.Multiple && had:
		return rest
	case poll.Multiple:
		return append(rest, pressed)
	case had:
		return nil
	}
	return []string{pressed}
}

// chatcmd002Ballot is the anonymous ballot to cast from what is chosen on the
// page: only options this poll has, no more than it allows, and never an empty
// one.
func chatcmd002Ballot(poll chat.Chatcmd002Poll, checked []string) ([]string, bool) {
	var options []string
	for _, id := range checked {
		for _, option := range poll.Options {
			if option.ID == id && id != "" {
				options = append(options, id)
			}
		}
	}
	if len(options) == 0 || (len(options) > 1 && !poll.Multiple) {
		return nil, false
	}
	return options, true
}

func chatcmd002Action(m Model, action, post, extra string) bool {
	if !strings.HasPrefix(action, "chatcmd002-") {
		return false
	}
	if action == "chatcmd002-jump" {
		chatcmd002RevealMessage(post)
		return true
	}
	view, ok := m.Chatcmd002Views[post]
	if !ok {
		return true
	}
	revision := view.Revision
	for _, msg := range append(append([]Message{}, m.Messages...), m.ThreadMessages...) {
		if msg.ID == post {
			revision = msg.Revision
		}
	}
	if m.ThreadParent != nil && m.ThreadParent.ID == post {
		revision = m.ThreadParent.Revision
	}
	switch action {
	case "chatcmd002-add-tasks":
		if m.Chatcmd002.AddToTasks != nil {
			m.Chatcmd002.AddToTasks(post, extra)
		}
	case "chatcmd002-move-channel":
		if m.Chatcmd002.MoveToChannel != nil {
			m.Chatcmd002.MoveToChannel(post, extra)
		}
	case "chatcmd002-vote":
		if m.Chatcmd002.Mutate == nil || view.Card.Poll == nil || view.Card.Poll.Anonymous {
			return true
		}
		m.Chatcmd002.Mutate(post, revision, chat.Chatcmd002Mutation{Operation: "VOTE", Options: chatcmd002NextVote(*view.Card.Poll, view.MyOptions, extra)})
	case "chatcmd002-cast":
		if m.Chatcmd002.Mutate == nil || view.Card.Poll == nil || !view.Card.Poll.Anonymous || view.Voted {
			return true
		}
		if options, ok := chatcmd002Ballot(*view.Card.Poll, chatcmd002CheckedChoices(post)); ok {
			m.Chatcmd002.Mutate(post, revision, chat.Chatcmd002Mutation{Operation: "VOTE", Options: options})
		}
	case "chatcmd002-tick":
		if m.Chatcmd002.Mutate == nil || view.Card.Todo == nil || !view.CanTick[extra] {
			return true
		}
		for _, item := range view.Card.Todo.Items {
			if item.ID == extra {
				m.Chatcmd002.Mutate(post, revision, chat.Chatcmd002Mutation{Operation: "TICK", ItemID: extra, Completed: !item.Completed})
			}
		}
	case "chatcmd002-close":
		if m.Chatcmd002.Mutate != nil && view.CanManage && (extra == "CLOSE" || extra == "REOPEN") {
			m.Chatcmd002.Mutate(post, revision, chat.Chatcmd002Mutation{Operation: extra})
		}
	case "chatcmd002-add-option":
		if chatcmd002AddOption(m, post, view, revision, chatcmd002FieldValues(post)["add"]) {
			chatcmd002ClearField(post, "add")
		}
	case "chatcmd002-edit-save":
		chatcmd002SaveEdit(m, post, view, revision, chatcmd002FieldValues(post))
	}
	return true
}

func chatcmd002PersonName(m Model, home, id string) string {
	for _, member := range m.Members {
		if member.ID == id && (member.HomeTenantID == home || member.HomeTenantID == "" || home == "") {
			if member.ID == m.CurrentUser {
				return m.t(KeyYou)
			}
			if member.Name != "" && member.Name != member.ID {
				return member.Name
			}
		}
	}
	return chatcmd003Text(m, "former-member")
}

// The card sits in the message column and never grows wider than it. Rows wrap
// rather than overflow, and every colour is a token so both themes are right.
const chatcmd002Styles = `.chatcmd002-card,.chatcmd003-preview{box-sizing:border-box;min-width:0;width:100%;max-width:520px;border:1px solid var(--hcm-color-border,var(--line));border-radius:var(--hcm-radius-control);padding:12px 14px;overflow-wrap:anywhere;background:var(--hcm-color-surface,var(--surface));color:var(--hcm-color-text,var(--ink))}` +
	`.chatcmd002-card{display:grid;gap:8px;margin-block:4px}.chatcmd002-head{display:flex;align-items:flex-start;gap:8px;min-width:0}.chatcmd002-head .chat-icon{flex:none;width:18px;height:18px;margin-block-start:2px;color:var(--muted)}` +
	`.chatcmd002-title{flex:1 1 auto;min-width:0;margin:0;font-size:1rem;line-height:1.35;font-weight:600}.chatcmd002-closed{flex:none;padding:1px 8px;border-radius:999px;background:var(--soft);color:var(--muted);font-size:.75rem;font-weight:600}` +
	`.chatcmd002-meta,.chatcmd002-note,.chatcmd002-voters,.chatcmd002-by{margin:0;color:var(--muted);font-size:.8125rem;line-height:1.4}` +
	`.chatcmd002-options,.chatcmd002-tasks{list-style:none;padding:0;margin:0;display:grid;gap:6px}.chatcmd002-option{display:grid;gap:2px;min-width:0}` +
	`.chatcmd002-vote,.chatcmd002-choice{box-sizing:border-box;display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:center;column-gap:10px;row-gap:6px;width:100%;min-height:40px;padding:8px 10px;border:1px solid var(--hcm-color-border,var(--line));border-radius:var(--hcm-radius-control);background:var(--hcm-color-surface,var(--surface));color:inherit;font:inherit;text-align:start;cursor:pointer}` +
	`.chatcmd002-choice{grid-template-columns:auto minmax(0,1fr) auto}.chatcmd002-choice input{margin:0}` +
	`.chatcmd002-vote:hover:not(:disabled),.chatcmd002-choice:hover{border-color:var(--hcm-color-brand-primary,var(--accent));background:var(--soft)}.chatcmd002-vote:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}` +
	`.chatcmd002-vote:disabled{cursor:default;opacity:1}.chatcmd002-vote.selected{border-color:var(--hcm-color-brand-primary,var(--accent));box-shadow:inset 0 0 0 1px var(--hcm-color-brand-primary,var(--accent))}` +
	`.chatcmd002-option-text{min-width:0;font-weight:600}.chatcmd002-count{color:var(--muted);font-size:.8125rem;font-variant-numeric:tabular-nums;white-space:nowrap}` +
	`.chatcmd002-bar{grid-column:1 / -1;display:block;width:100%;min-width:0;height:6px;accent-color:var(--hcm-color-brand-primary,var(--accent))}` +
	`.chatcmd002-voters{padding-inline:10px}` +
	`.chatcmd002-task{display:flex;align-items:center;flex-wrap:wrap;column-gap:8px;row-gap:2px;min-width:0;min-height:32px}.chatcmd002-task-text{flex:1 1 12ch;min-width:0}.chatcmd002-task.done .chatcmd002-task-text{color:var(--muted);text-decoration:line-through}` +
	`.chatcmd002-tick{flex:none;display:inline-grid;place-items:center;width:20px;height:20px;padding:0;border:1.5px solid var(--muted);border-radius:4px;background:var(--hcm-color-surface,var(--surface));color:var(--hcm-color-on-brand,#fff);cursor:pointer}` +
	`.chatcmd002-tick[aria-checked="true"]{border-color:var(--hcm-color-brand-primary,var(--accent));background:var(--hcm-color-brand-primary,var(--accent))}.chatcmd002-tick .chat-icon{width:14px;height:14px}.chatcmd002-tick:disabled{cursor:default;opacity:.6}.chatcmd002-tick:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}` +
	`.chatcmd002-chip{flex:none;padding:1px 8px;border-radius:999px;background:var(--soft);color:var(--muted);font-size:.75rem;white-space:nowrap}` +
	`.chatcmd002-foot{display:flex;align-items:center;flex-wrap:wrap;gap:8px 12px}.chatcmd002-link{padding:0;border:0;background:none;color:var(--hcm-color-brand-primary,var(--accent));font:inherit;font-size:.8125rem;font-weight:600;cursor:pointer}.chatcmd002-link:hover{text-decoration:underline}.chatcmd002-link:disabled{color:var(--muted);cursor:default;text-decoration:none}` +
	`@media(pointer:coarse){.chatcmd002-vote,.chatcmd002-choice{min-height:44px}.chatcmd002-tick{width:24px;height:24px}}` +
	`.chatcmd003-preview{display:grid;gap:10px;max-width:none;margin:8px 0}.chatcmd003-preview h3{margin:0;font-size:.9375rem}.chatcmd003-preview .chatcmd002-card{max-width:none;margin:0;background:var(--soft)}` +
	`.chatcmd003-settings{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,150px),1fr));gap:8px}.chatcmd003-setting{display:grid;gap:4px;min-width:0;font-size:.8125rem}.chatcmd003-setting label{color:var(--muted)}.chatcmd003-setting select,.chatcmd003-setting input{width:100%;min-width:0}` +
	`.chatcmd003-preview textarea,.chatcmd003-preview input{box-sizing:border-box;display:block;width:100%;max-width:100%}.chatcmd003-changes{display:grid;gap:2px;margin:0;padding:0;list-style:none;color:var(--muted);font-size:.8125rem}.chatcmd003-changes del{text-decoration:line-through}.chatcmd003-changes ins{text-decoration:none;color:var(--hcm-color-text,var(--ink))}` +
	`.chatcmd003-note{margin:0;color:var(--muted);font-size:.8125rem}.chatcmd003-row{display:flex;align-items:center;flex-wrap:wrap;gap:8px}.chatcmd003-actions{display:flex;flex-wrap:wrap;gap:8px}`
