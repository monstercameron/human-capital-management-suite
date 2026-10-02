package chatui

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type Chatcmd002Callbacks struct {
	Post          func(conversation, parent string, card chat.Chatcmd002Card, done func(error))
	Mutate        func(post string, revision uint64, mutation chat.Chatcmd002Mutation)
	Tidy          func(conversation string, draft chat.Chatcmd003Draft, done func(chat.Chatcmd003Draft, error))
	AddToTasks    func(post, item string)
	MoveToChannel func(post, item string)
}
type chatcmd003Preview struct {
	Open, Editing, Busy          bool
	Conversation, Parent, Target string
	Draft                        chat.Chatcmd003Draft
	Generation                   uint64
	Error                        string
}

func chatcmd003Location(m Model) *time.Location {
	if zone, err := time.LoadLocation(m.Chatcmd002TimeZone); err == nil && m.Chatcmd002TimeZone != "" {
		return zone
	}
	return time.Local
}
func chatcmd003Parse(m Model, kind, raw string) (chat.Chatcmd003Draft, error) {
	now := time.Now()
	if m.Chatcmd002TimeZone != "" {
		if zone, err := time.LoadLocation(m.Chatcmd002TimeZone); err == nil {
			now = now.In(zone)
		}
	}
	if kind == "poll" {
		return chat.Chatcmd003ParsePoll(raw, now, chat.Chatcmd004ResolveDate)
	}
	var members []chat.Chatcmd004Member
	for _, member := range m.Members {
		members = append(members, chat.Chatcmd004Member{HomeTenantID: member.HomeTenantID, ID: member.ID, Name: member.Name})
	}
	return chat.Chatcmd004ParseTodo(raw, members, m.CurrentUser, now, chat.Chatcmd004ResolveDate)
}
func chatcmd003Open(m Model, local localStore, kind, raw, target string) {
	draft, err := chatcmd003Parse(m, kind, raw)
	if draft.Card.Title == "" {
		draft.Card.Title = chatcmd003Text(m, kind)
	}
	if draft.Original.Title == "" {
		draft.Original.Title = draft.Card.Title
	}
	local.update(func(u *localUI) {
		u.chatcmd003 = chatcmd003Preview{Open: true, Editing: strings.TrimSpace(raw) == "", Conversation: m.SelectedID, Target: target, Draft: draft, Generation: u.chatcmd003.Generation + 1}
		if target == "thread-composer" {
			u.chatcmd003.Parent = m.ThreadParentID
		}
		if err != nil {
			u.chatcmd003.Error = "parse"
			u.chatcmd003.Editing = true
		}
		u.commandMenu = composerCommandMenu{}
	})
}
func chatcmd003Send(m Model, local localStore, target, body string) bool {
	name, args, ok := parseComposerCommand(body)
	name = strings.ToLower(name)
	if !ok || (name != "poll" && name != "todo") {
		return false
	}
	if !chatcmd003Available(m) {
		local.update(func(u *localUI) { u.composerNotice = chatcmd003Text(m, "unavailable") })
		return true
	}
	chatcmd003Open(m, local, name, args, target)
	return true
}
func chatcmd003Available(m Model) bool {
	return m.SelectedID != "" && !m.selected().Agent && m.Chatcmd002.Post != nil
}
func chatcmd003RegistryRun(rt composerCommandRuntime, _ string) {
	if rt.Notice != nil {
		rt.Notice(chatcmd003Text(rt.Model, "preview"))
	}
}

func chatcmd003ReadSettings(m Model, c chat.Chatcmd002Card) (chat.Chatcmd002Card, error) {
	raw, _ := json.Marshal(c)
	var next chat.Chatcmd002Card
	_ = json.Unmarshal(raw, &next)
	if next.Poll != nil {
		if value := domValue("chatcmd003-multiple"); value != "" {
			next.Poll.Multiple = value == "yes"
		}
		if value := domValue("chatcmd003-anonymous"); value != "" {
			next.Poll.Anonymous = value == "yes"
		}
		if value := domValue("chatcmd003-results"); value != "" {
			next.Poll.Results = value
		}
		closes := strings.TrimSpace(domValue("chatcmd003-closes"))
		if closes == "never" {
			next.Poll.ClosesAt = nil
		} else if closes != "" {
			due, err := chat.Chatcmd004ResolveDate(closes, time.Now().In(chatcmd003Location(m)))
			if err != nil {
				return c, err
			}
			next.Poll.ClosesAt = &due
		}
	}
	if next.Todo != nil {
		if value := domValue("chatcmd003-tick"); value != "" {
			next.Todo.Tick = value
		}
	}
	return next, next.Validate()
}
func chatcmd003SettingsChanged(a, b chat.Chatcmd002Card) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return !bytes.Equal(left, right)
}
func chatcmd003Action(m Model, local localStore, action, id, extra string) bool {
	if action == "composer-add" && (extra == "poll" || extra == "todo") && chatcmd003Available(m) {
		closeComposerAddMenu(false)
		chatcmd003Open(m, local, extra, "", id)
		return true
	}
	if !strings.HasPrefix(action, "chatcmd003-") {
		return chatcmd002Action(m, action, id, extra)
	}
	state := local.get().chatcmd003
	if !state.Open || state.Conversation != m.SelectedID || (state.Busy && action != "chatcmd003-cancel") {
		return true
	}
	switch action {
	case "chatcmd003-cancel":
		raw := state.Draft.Raw
		if state.Editing {
			if edited, _, present := composerSelection("chatcmd003-source"); present {
				raw = edited
			}
		}
		restored := "/" + state.Draft.Card.Kind + " " + raw
		setDOMValue(state.Target, restored)
		if state.Target == "chat-composer" && m.Callbacks.DraftChanged != nil {
			m.Callbacks.DraftChanged(state.Conversation, restored)
		}
		local.update(func(u *localUI) { u.chatcmd003.Open = false; u.chatcmd003.Generation++ })
		focusField(state.Target)
	case "chatcmd003-settings":
		card, err := chatcmd003ReadSettings(m, state.Draft.Card)
		local.update(func(u *localUI) {
			if err != nil {
				u.chatcmd003.Error = "date"
			} else {
				u.chatcmd003.Draft.Card = card
				u.chatcmd003.Error = ""
			}
		})
		if err == nil {
			setDOMValue("chatcmd003-closes", "")
		}
	case "chatcmd003-confirm":
		local.update(func(u *localUI) { u.chatcmd003.Draft.Issues = nil })
	case "chatcmd003-edit":
		local.update(func(u *localUI) { u.chatcmd003.Editing = true })
	case "chatcmd003-original":
		local.update(func(u *localUI) {
			u.chatcmd003.Draft.Card = u.chatcmd003.Draft.Original
			u.chatcmd003.Draft.Changes = nil
			u.chatcmd003.Error = ""
		})
	case "chatcmd003-preview":
		draft, err := chatcmd003Parse(m, state.Draft.Card.Kind, domValue("chatcmd003-source"))
		if draft.Card.Title == "" {
			draft.Card.Title = chatcmd003Text(m, draft.Card.Kind)
		}
		if draft.Original.Title == "" {
			draft.Original.Title = draft.Card.Title
		}
		local.update(func(u *localUI) {
			u.chatcmd003.Draft = draft
			u.chatcmd003.Error = ""
			u.chatcmd003.Generation++
			if err != nil {
				u.chatcmd003.Error = "parse"
			} else {
				u.chatcmd003.Editing = false
			}
		})
	case "chatcmd003-tidy":
		if m.Chatcmd002.Tidy == nil {
			return true
		}
		local.update(func(u *localUI) { u.chatcmd003.Busy = true; u.chatcmd003.Error = "" })
		m.Chatcmd002.Tidy(state.Conversation, state.Draft, func(draft chat.Chatcmd003Draft, err error) {
			if current := local.get().chatcmd003; !current.Open || current.Generation != state.Generation {
				return
			}
			local.update(func(u *localUI) {
				u.chatcmd003.Busy = false
				if err != nil {
					u.chatcmd003.Error = "tidy-fallback"
				} else {
					u.chatcmd003.Draft = draft
				}
			})
		})
	case "chatcmd003-post":
		if m.Chatcmd002.Post == nil || state.Editing || state.Draft.Card.Validate() != nil || len(state.Draft.Issues) != 0 {
			return true
		}
		card, err := chatcmd003ReadSettings(m, state.Draft.Card)
		if err != nil {
			local.update(func(u *localUI) { u.chatcmd003.Error = "date" })
			return true
		}
		if chatcmd003SettingsChanged(state.Draft.Card, card) {
			local.update(func(u *localUI) { u.chatcmd003.Draft.Card = card; u.chatcmd003.Error = "review-settings" })
			setDOMValue("chatcmd003-closes", "")
			return true
		}

		local.update(func(u *localUI) { u.chatcmd003.Busy = true; u.chatcmd003.Error = "" })
		m.Chatcmd002.Post(state.Conversation, state.Parent, card, func(err error) {
			if current := local.get().chatcmd003; !current.Open || current.Generation != state.Generation {
				return
			}
			local.update(func(u *localUI) {
				u.chatcmd003.Busy = false
				if err != nil {
					u.chatcmd003.Error = "post-error"
				} else {
					u.chatcmd003.Open = false
					u.chatcmd003.Generation++
				}
			})
			if err == nil {
				setDOMValue(state.Target, "")
				if m.Callbacks.DraftChanged != nil && state.Target == "chat-composer" {
					m.Callbacks.DraftChanged(state.Conversation, "")
				}
				focusField(state.Target)
			}
		})
	}
	return true
}

func chatcmd003Select(m Model, id, label, value string, options ...string) ui.Node {
	var rows []ui.Node
	for _, option := range options {
		rows = append(rows, html.Option(html.Props{Value: option, Selected: option == value, Text: chatcmd003Text(m, option)}))
	}
	return html.Div(html.Props{Class: "chatcmd003-setting"}, html.Label(html.Props{For: id, Text: chatcmd003Text(m, label)}), html.Select(html.Props{ID: id, Class: "chat-input"}, rows...))
}
func chatcmd003PreviewView(m Model, state chatcmd003Preview) ui.Node {
	if !state.Open || state.Conversation != m.SelectedID {
		return nil
	}
	button := func(action, key string, disabled bool) ui.Node {
		return actionButton("button secondary small", "chatcmd003-"+action, "", chatcmd003Text(m, key), disabled, ui.Text(chatcmd003Text(m, key)))
	}
	children := []ui.Node{html.H3(html.Props{ID: "chatcmd003-heading", Text: chatcmd003Text(m, "preview")})}
	sourceAria := map[string]string{"describedby": "chatcmd003-usage"}
	if state.Error != "" {
		sourceAria["describedby"] += " chatcmd003-error"
		sourceAria["invalid"] = "true"
	}
	if state.Editing {
		children = append(children, html.Label(html.Props{For: "chatcmd003-source", Text: chatcmd003Text(m, "draft")}), html.Textarea(html.Props{ID: "chatcmd003-source", Class: "chat-input", Rows: 5, MaxLength: 12000, Dir: "auto", Aria: sourceAria}, ui.Text(state.Draft.Raw)), html.P(html.Props{ID: "chatcmd003-usage", Class: "muted", Dir: "auto", Text: chatcmd003Text(m, state.Draft.Card.Kind+"-usage")}), button("preview", "preview", state.Busy))
	} else {
		children = append(children, Chatcmd002RenderCard(m, "", chat.Chatcmd002View{Card: state.Draft.Card, ResultsVisible: true}, true))
		if state.Draft.Card.Poll != nil {
			p := state.Draft.Card.Poll
			choice := "no"
			if p.Multiple {
				choice = "yes"
			}
			anonymous := "no"
			if p.Anonymous {
				anonymous = "yes"
			}
			children = append(children, html.Div(html.Props{Class: "chatcmd003-settings"}, chatcmd003Select(m, "chatcmd003-multiple", "multiple", choice, "no", "yes"), chatcmd003Select(m, "chatcmd003-anonymous", "anonymous", anonymous, "no", "yes"), chatcmd003Select(m, "chatcmd003-results", "results", p.Results, "always", "after-voting", "after-closing")), html.Label(html.Props{For: "chatcmd003-closes", Text: chatcmd003Text(m, "closes")}), html.Input(html.Props{ID: "chatcmd003-closes", Type: "text", Class: "chat-input", Placeholder: chatcmd003Text(m, "closes-hint")}))
		}
		if state.Draft.Card.Todo != nil {
			children = append(children, chatcmd003Select(m, "chatcmd003-tick", "tick-policy", state.Draft.Card.Todo.Tick, "anyone", "assignee", "author"))
		}
		children = append(children, button("settings", "apply-settings", state.Busy), button("edit", "edit", state.Busy))
	}
	if len(state.Draft.Changes) > 0 {
		children = append(children, html.P(html.Props{Role: "status", Text: strings.ReplaceAll(chatcmd003Text(m, "changes"), "{n}", strconv.Itoa(len(state.Draft.Changes)))}))
		for _, change := range state.Draft.Changes {
			children = append(children, html.P(html.Props{Dir: "auto", Text: change.Before + " â†’ " + change.After}))
		}
		children = append(children, button("original", "original", state.Busy))
	}
	for _, issue := range state.Draft.Issues {
		key := issue
		if issue == "assignee" {
			key = "ambiguous-assignee"
		}
		children = append(children, html.P(html.Props{Role: "status", Text: chatcmd003Text(m, key)}))
	}
	if len(state.Draft.Issues) > 0 {
		children = append(children, button("confirm", "confirm", state.Busy || state.Editing))
	}
	if state.Error != "" {
		children = append(children, html.P(html.Props{ID: "chatcmd003-error", Role: "alert", Text: chatcmd003Text(m, state.Error)}))
	}
	if state.Busy {
		children = append(children, html.P(html.Props{Role: "status", Aria: map[string]string{"live": "polite"}, Text: chatcmd003Text(m, "working")}))
	}
	if m.Chatcmd002.Tidy != nil {
		children = append(children, html.P(html.Props{Class: "muted", Text: chatcmd003Text(m, "ai-notice") + " " + chatcmd003Text(m, "cost")}), button("tidy", "tidy", state.Busy || state.Editing))
	}
	children = append(children, html.Div(html.Props{Class: "chatcmd003-actions"}, actionButton("button small", "chatcmd003-post", "", chatcmd003Text(m, "post"), state.Busy || state.Editing || state.Draft.Card.Validate() != nil || len(state.Draft.Issues) != 0 || m.Chatcmd002.Post == nil, ui.Text(chatcmd003Text(m, "post"))), button("cancel", "cancel", false)))
	return html.Section(html.Props{Class: "chatcmd003-preview", Role: "region", Dir: agentReplyDirection(m.Locale), Aria: map[string]string{"labelledby": "chatcmd003-heading"}}, children...)
}

func chatcmd003PreviewFor(m Model, state chatcmd003Preview, target string) ui.Node {
	if state.Target != target {
		return nil
	}
	return chatcmd003PreviewView(m, state)
}

// Chatcmd003RenderPreview exposes the same card preview to command producers.
func Chatcmd003RenderPreview(m Model, draft chat.Chatcmd003Draft) ui.Node {
	return chatcmd003PreviewView(m, chatcmd003Preview{Open: true, Conversation: m.SelectedID, Draft: draft})
}
