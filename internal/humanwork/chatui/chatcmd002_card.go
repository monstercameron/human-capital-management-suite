package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"strconv"
	"strings"
	"time"
)

type Chatcmd002View = chat.Chatcmd002View

func Chatcmd002RenderCard(m Model, post string, view chat.Chatcmd002View, preview bool) ui.Node {
	c := view.Card
	closed := c.ClosedAt != nil || c.Poll != nil && c.Poll.ClosesAt != nil && !time.Now().Before(*c.Poll.ClosesAt)
	children := []ui.Node{html.H4(html.Props{Class: "chatcmd002-title", Dir: "auto", Text: c.Title})}
	if closed {
		stamp := c.ClosedAt
		if stamp == nil && c.Poll != nil {
			stamp = c.Poll.ClosesAt
		}
		if stamp != nil {
			children = append(children, html.Time(html.Props{Class: "muted", Text: stamp.In(chatcmd003Location(m)).Format("2006-01-02 15:04")}))
		}
		children = append(children, html.P(html.Props{Class: "muted", Text: chatcmd003Text(m, "closed")}))
	}
	if c.Poll != nil {
		if c.Poll.Multiple {
			children = append(children, html.P(html.Props{Class: "muted", Text: chatcmd003Text(m, "multiple")}))
		}
		if c.Poll.Anonymous {
			children = append(children, html.P(html.Props{Class: "muted", Text: chatcmd003Text(m, "anonymous")}))
		}
		if !closed && c.Poll.ClosesAt != nil {
			children = append(children, html.Time(html.Props{Class: "muted", Text: chatcmd003Text(m, "closes") + ": " + c.Poll.ClosesAt.In(chatcmd003Location(m)).Format("2006-01-02 15:04")}))
		}
		total := 0
		for _, o := range c.Poll.Options {
			total += o.Count
		}
		var rows []ui.Node
		for _, o := range c.Poll.Options {
			mine := false
			for _, id := range view.MyOptions {
				mine = mine || id == o.ID
			}
			label := chatcmd003Text(m, "vote")
			if mine {
				label = chatcmd003Text(m, "voted")
			}
			parts := []ui.Node{html.Button(html.Props{Type: "button", Class: "button secondary small", Disabled: preview || closed || m.Chatcmd002.Mutate == nil, Data: map[string]string{"action": "chatcmd002-vote", "id": post, "extra": o.ID}, Aria: map[string]string{"pressed": boolString(mine), "label": label + ": " + o.Text}, Text: label}), html.Span(html.Props{Dir: "auto", Text: o.Text})}
			if preview || view.ResultsVisible {
				percent := pollPercent(o.Count, total)
				parts = append(parts, html.Progress(html.Props{Raw: map[string]any{"value": percent, "max": 100}, Aria: map[string]string{"label": o.Text}}), html.Span(html.Props{Text: strconv.Itoa(o.Count) + " · " + strconv.Itoa(percent) + "%"}))
				if !c.Poll.Anonymous {
					for _, name := range view.Voters[o.ID] {
						parts = append(parts, html.Span(html.Props{Dir: "auto", Text: name}))
					}
				}
			}
			rows = append(rows, html.Li(html.Props{Class: "chatcmd002-option"}, parts...))
		}
		children = append(children, html.Ul(html.Props{Class: "chatcmd002-options"}, rows...))
		if !preview && !view.ResultsVisible {
			children = append(children, html.P(html.Props{Role: "status", Text: chatcmd003Text(m, "waiting-results")}))
		}
	}
	if c.Todo != nil {
		var rows []ui.Node
		for _, item := range c.Todo.Items {
			label := chatcmd003Text(m, "tick")
			if item.Completed {
				label = chatcmd003Text(m, "untick")
			}
			parts := []ui.Node{html.Button(html.Props{Type: "button", Role: "checkbox", Class: "button secondary small", Disabled: preview || closed || !view.CanTick[item.ID] || m.Chatcmd002.Mutate == nil, Data: map[string]string{"action": "chatcmd002-tick", "id": post, "extra": item.ID}, Aria: map[string]string{"checked": boolString(item.Completed), "label": label + ": " + item.Text}, Text: label}), html.Span(html.Props{Dir: "auto", Text: item.Text})}
			if item.AssigneeID != "" {
				parts = append(parts, html.Span(html.Props{Class: "muted", Dir: "auto", Text: strings.ReplaceAll(chatcmd003Text(m, "assigned"), "{name}", chatcmd002PersonName(m, item.AssigneeHomeTenantID, item.AssigneeID))}))
			}
			if item.DueAt != nil {
				parts = append(parts, html.Time(html.Props{Class: "muted", Text: strings.ReplaceAll(chatcmd003Text(m, "due"), "{date}", item.DueAt.In(chatcmd003Location(m)).Format("2006-01-02"))}))
			}
			if item.Completed {
				name := chatcmd002PersonName(m, item.CompletedByHomeTenantID, item.CompletedBySubjectID)
				text := strings.ReplaceAll(chatcmd003Text(m, "completed"), "{name}", name)
				text = strings.ReplaceAll(text, "{date}", time.Unix(item.CompletedAtUnix, 0).In(chatcmd003Location(m)).Format("2006-01-02 15:04"))
				parts = append(parts, html.Span(html.Props{Class: "muted", Dir: "auto", Text: text}))
			}
			if !preview && m.Chatcmd002.AddToTasks != nil {
				parts = append(parts, html.Button(html.Props{Type: "button", Class: "button secondary small", Data: map[string]string{"action": "chatcmd002-add-tasks", "id": post, "extra": item.ID}, Text: chatcmd003Text(m, "add-tasks")}))
			}
			if !preview && m.Chatcmd002.MoveToChannel != nil {
				parts = append(parts, html.Button(html.Props{Type: "button", Class: "button secondary small", Data: map[string]string{"action": "chatcmd002-move-channel", "id": post, "extra": item.ID}, Text: chatcmd003Text(m, "move-channel")}))
			}
			rows = append(rows, html.Li(html.Props{Class: "chatcmd002-task"}, parts...))
		}
		children = append(children, html.Ul(html.Props{Class: "chatcmd002-tasks"}, rows...))
	}
	if !preview && view.CanManage {
		label, operation := "close", "CLOSE"
		if closed {
			label, operation = "reopen", "REOPEN"
		}
		children = append(children, html.Button(html.Props{Class: "button secondary small", Type: "button", Disabled: m.Chatcmd002.Mutate == nil, Data: map[string]string{"action": "chatcmd002-close", "id": post, "extra": operation}, Text: chatcmd003Text(m, label)}))
	}
	return html.Section(html.Props{Class: "chatcmd002-card", Dir: agentReplyDirection(m.Locale), Aria: map[string]string{"label": chatcmd003Text(m, c.Kind)}}, children...)
}
func chatcmd002ProjectedBody(m Model, msg Message, fallback ui.Node) ui.Node {
	c, ok := chat.Chatcmd002Decode(msg.Body)
	if !ok {
		return fallback
	}
	view, loaded := m.Chatcmd002Views[msg.ID]
	if !loaded {
		view = chat.Chatcmd002View{Card: c, ResultsVisible: c.Poll != nil && c.Poll.Results == "always"}
		m.Chatcmd002.Mutate = nil
	}
	return Chatcmd002RenderCard(m, msg.ID, view, false)
}
func chatcmd002Action(m Model, action, post, extra string) bool {
	if !strings.HasPrefix(action, "chatcmd002-") {
		return false
	}
	view, ok := m.Chatcmd002Views[post]
	if !ok {
		return true
	}
	revision := uint64(0)
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
		if m.Chatcmd002.Mutate == nil || view.Card.Poll == nil {
			return true
		}
		options := []string{extra}
		if view.Card.Poll.Multiple {
			options = nil
			found := false
			for _, id := range view.MyOptions {
				if id == extra {
					found = true
				} else {
					options = append(options, id)
				}
			}
			if !found {
				options = append(options, extra)
			}
		}
		m.Chatcmd002.Mutate(post, revision, chat.Chatcmd002Mutation{Operation: "VOTE", Options: options})
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
		if m.Chatcmd002.Mutate != nil && view.CanManage {
			m.Chatcmd002.Mutate(post, revision, chat.Chatcmd002Mutation{Operation: extra})
		}
	}
	return true
}
func chatcmd002PersonName(m Model, home, id string) string {
	for _, member := range m.Members {
		if member.ID == id && member.HomeTenantID == home {
			return member.Name
		}
	}
	return chatcmd003Text(m, "former-member")
}

const chatcmd002Styles = `.chatcmd002-card,.chatcmd003-preview{min-width:0;max-width:100%;border:1px solid var(--hcm-color-border,var(--line));border-radius:var(--hcm-radius-control);padding:12px;overflow-wrap:anywhere;background:var(--hcm-color-surface,var(--surface));color:var(--hcm-color-text,var(--ink))}.chatcmd002-title{margin:0 0 8px}.chatcmd002-options,.chatcmd002-tasks{list-style:none;padding:0;margin:0;display:grid;gap:8px}.chatcmd002-option,.chatcmd002-task{display:flex;align-items:center;flex-wrap:wrap;gap:8px;min-width:0}.chatcmd002-option progress{max-width:100%;flex:1 1 80px;min-width:0;accent-color:var(--hcm-color-primary,var(--accent))}.chatcmd002-task>span{overflow-wrap:anywhere}.chatcmd003-preview{margin:8px 0}.chatcmd003-preview h3{margin:0 0 10px}.chatcmd003-settings{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,140px),1fr));gap:8px;margin:10px 0}.chatcmd003-setting{min-width:0}.chatcmd003-setting select{width:100%;min-width:0}.chatcmd003-preview textarea,.chatcmd003-preview input{display:block;width:100%;max-width:100%}.chatcmd003-actions{display:flex;flex-wrap:wrap;gap:8px;margin-block-start:12px}`
