package chatui

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// /poll and /todo make the composer itself the draft of a card, and Poll and
// To-do list in the composer's add menu write that command word into the
// composer, so there is one field and one way in. The preview is drawn above
// the composer from what the composer holds, on every keystroke, by the tidy
// step that needs no model (chat.Chatcmd003Tidy). Enter posts the card the
// preview shows; Escape or Cancel closes the preview and leaves the composer
// empty. A line that starts with one of the two commands is never sent as an
// ordinary message. Because the text itself says it is a card, a draft is
// still one after a reload or a visit to another conversation. "Tidy with AI"
// is a second, optional pass offered only where the client binds one.

type Chatcmd002Callbacks struct {
	Post          func(conversation, parent string, card chat.Chatcmd002Card, done func(error))
	Mutate        func(post string, revision uint64, mutation chat.Chatcmd002Mutation)
	Tidy          func(conversation string, draft chat.Chatcmd003Draft, done func(chat.Chatcmd003Draft, error))
	AddToTasks    func(post, item string)
	MoveToChannel func(post, item string)
	// CloseChannelPoll ends the channel's standing poll (CHATBUG-074).
	CloseChannelPoll func()
}

type chatcmd003Preview struct {
	Open, Busy bool
	// Posting says the card is on its way to the server. Until the answer is
	// here the preview does not follow the composer.
	Posting bool
	// Typed keeps the card as typed ("Use what I typed") while the draft changes.
	Typed bool
	// Standing says the draft is the channel's standing poll being moved into
	// the conversation: posting it closes that poll (chatbug057_move.go).
	Standing bool
	// Touched says the person has typed, opened or closed a preview in this
	// conversation. Until then a saved draft that is a /poll or /todo line (a
	// page reloaded in the middle of one) shows its preview again.
	Touched                      bool
	Conversation, Parent, Target string
	Draft                        chat.Chatcmd003Draft
	// Settings is what the person chose in the preview's controls. A choice
	// holds while the text is read again.
	Settings   map[string]string
	Generation uint64
	Error      string
}

func chatcmd003Location(m Model) *time.Location {
	if zone, err := time.LoadLocation(m.Chatcmd002TimeZone); err == nil && m.Chatcmd002TimeZone != "" {
		return zone
	}
	return time.Local
}

// chatcmd003Parse reads a command's text into a draft and tidies it. The draft
// keeps the card as typed in Original, so "Use what I typed" can put it back.
func chatcmd003Parse(m Model, kind, raw string) (chat.Chatcmd003Draft, error) {
	now := time.Now().In(chatcmd003Location(m))
	var draft chat.Chatcmd003Draft
	var err error
	if kind == "poll" {
		draft, err = chat.Chatcmd003ParsePoll(raw, now, chat.Chatcmd004ResolveDate)
	} else {
		var members []chat.Chatcmd004Member
		for _, member := range m.Members {
			members = append(members, chat.Chatcmd004Member{HomeTenantID: member.HomeTenantID, ID: member.ID, Name: member.Name})
		}
		draft, err = chat.Chatcmd004ParseTodo(raw, members, m.CurrentUser, now, chat.Chatcmd004ResolveDate)
	}
	if err != nil {
		return draft, err
	}
	// A list, or a run of options with no question, still needs a title to post.
	if draft.Card.Title == "" {
		draft.Card.Title = chatcmd003Text(m, kind)
		draft.Original.Title = draft.Card.Title
	}
	return chat.Chatcmd003Tidy(draft), nil
}

func chatcmd003Available(m Model) bool {
	return m.SelectedID != "" && !m.selected().Agent && m.Chatcmd002.Post != nil
}

// chatcmd003RegistryRun is what the registry runs for /poll and /todo when the
// preview did not take the line itself: it says a preview comes first.
func chatcmd003RegistryRun(rt composerCommandRuntime, _ string) {
	if rt.Notice != nil {
		rt.Notice(chatcmd003Text(rt.Model, "preview"))
	}
}

func chatcmd003CloneCard(c chat.Chatcmd002Card) chat.Chatcmd002Card {
	raw, _ := json.Marshal(c)
	var next chat.Chatcmd002Card
	_ = json.Unmarshal(raw, &next)
	return next
}

// chatcmd003Line reads a composer's text as a card command: "/poll " or
// "/todo " and what follows. The space after the word is what chooses the
// command, as it is for the command list, and the command must be one this
// composer can run here.
func chatcmd003Line(m Model, target, value string) (kind, raw string, ok bool) {
	line := chat.Chatcmd001ParseLine(value)
	if !line.Command || (line.Name != "poll" && line.Name != "todo") || !strings.ContainsAny(strings.TrimLeft(value, " \t\n"), " \t\n") {
		return "", "", false
	}
	if _, found := composerCommandsFor(target).lookup(m, line.Name); !found {
		return "", "", false
	}
	return line.Name, line.Raw, true
}

// chatcmd003Closes is the closing time a preset stands for, counted from now.
// Any other value keeps the date typed in the text (closes=2026-10-09).
func chatcmd003Closes(m Model, preset string, typed *time.Time) *time.Time {
	now := time.Now().In(chatcmd003Location(m))
	end := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 0, 0, now.Location())
	var at time.Time
	switch preset {
	case "never":
		return nil
	case "hour":
		at = now.Add(time.Hour)
	case "today":
		at = end
	case "tomorrow":
		at = end.AddDate(0, 0, 1)
	case "week":
		at = end.AddDate(0, 0, 7)
	default:
		return typed
	}
	return &at
}

// chatcmd003Apply puts the person's choices from the preview's controls on a
// card read from the text. The card is changed in place and returned.
func chatcmd003Apply(m Model, c chat.Chatcmd002Card, settings map[string]string) chat.Chatcmd002Card {
	if p := c.Poll; p != nil {
		if value, ok := settings["multiple"]; ok {
			p.Multiple = value == "yes"
		}
		if value, ok := settings["anonymous"]; ok {
			p.Anonymous = value == "yes"
		}
		if value := settings["results"]; value != "" {
			p.Results = value
		}
		if value := settings["add"]; value != "" {
			p.AddOptions = value
		}
		if value, ok := settings["closes"]; ok {
			p.ClosesAt = chatcmd003Closes(m, value, p.ClosesAt)
		}
	}
	if c.Todo != nil && settings["tick"] != "" {
		c.Todo.Tick = settings["tick"]
	}
	return c
}

// chatcmd003Structural keeps the changes that are not wording: what the
// reading took out of the text stays true of the card as typed.
func chatcmd003Structural(changes []chat.Chatcmd003Change) []chat.Chatcmd003Change {
	var kept []chat.Chatcmd003Change
	for _, change := range changes {
		if change.Kind != "" {
			kept = append(kept, change)
		}
	}
	return kept
}

// chatcmd003Draw reads raw again as a card of kind and puts the person's
// choices on it. Anything still on its way for the earlier text (a model tidy)
// is dropped by the new generation.
func chatcmd003Draw(m Model, state chatcmd003Preview, kind, raw string) chatcmd003Preview {
	draft, err := chatcmd003Parse(m, kind, raw)
	state.Error = ""
	if err != nil {
		state.Error = "parse"
	}
	if state.Typed {
		draft.Card, draft.Changes = chatcmd003CloneCard(draft.Original), chatcmd003Structural(draft.Changes)
	}
	draft.Card = chatcmd003Apply(m, draft.Card, state.Settings)
	state.Draft, state.Busy, state.Posting = draft, false, false
	state.Generation++
	return state
}

// chatcmd003Effective is the preview as the page shows it for target: the
// stored one, or, in a conversation the person has not typed in yet, the one
// its saved draft stands for.
func chatcmd003Effective(m Model, state chatcmd003Preview, target string) chatcmd003Preview {
	if state.Open || state.Touched || target != "chat-composer" {
		return state
	}
	kind, raw, ok := chatcmd003Line(m, target, m.Draft)
	if !ok {
		return state
	}
	return chatcmd003Draw(m, chatcmd003Preview{Open: true, Touched: true, Conversation: m.SelectedID, Target: target, Generation: state.Generation}, kind, raw)
}

// chatcmd003Adopt stores the preview the page shows for target, so the buttons
// of a preview drawn from a saved draft act on it. It returns that preview.
func chatcmd003Adopt(m Model, local localStore, target string) chatcmd003Preview {
	stored := local.get().chatcmd003
	state := chatcmd003Effective(m, stored, target)
	if state.Open && !stored.Open {
		local.update(func(u *localUI) { u.chatcmd003 = state })
	}
	return state
}

// chatcmd003Follow makes the preview what value, the text of the composer
// named by target, says it is, and reports whether a preview is open for that
// composer afterwards. A "/poll " or "/todo " line opens or updates one; any
// other text closes the one this composer had.
func chatcmd003Follow(m Model, local localStore, target, value string) bool {
	state := local.get().chatcmd003
	mine := state.Open && state.Target == target && state.Conversation == m.SelectedID
	if mine && state.Posting {
		return true
	}
	kind, raw, ok := chatcmd003Line(m, target, value)
	if !ok {
		switch {
		case mine || chatcmd003Effective(m, state, target).Open:
			local.update(func(u *localUI) { u.chatcmd003 = chatcmd003Preview{Touched: true, Generation: state.Generation + 1} })
		case !state.Open:
			// Nothing on the page changes, so no render is asked for.
			local.box.chatcmd003.Touched = true
		}
		return false
	}
	if mine && state.Draft.Card.Kind == kind && state.Draft.Raw == raw {
		return true
	}
	next := chatcmd003Preview{Open: true, Touched: true, Conversation: m.SelectedID, Target: target, Generation: state.Generation}
	if mine && state.Draft.Card.Kind == kind {
		next.Typed, next.Settings, next.Standing = state.Typed, state.Settings, state.Standing
	}
	if target == "thread-composer" {
		next.Parent = m.ThreadParentID
	}
	next = chatcmd003Draw(m, next, kind, raw)
	local.update(func(u *localUI) {
		u.chatcmd003 = next
		// The preview takes the place of an open channel card.
		u.tray = ""
	})
	return true
}

// chatcmd003Track follows the composer on every change of its text.
func chatcmd003Track(m Model, local localStore, target string) {
	chatcmd003Follow(m, local, target, domValue(target))
}

// chatcmd003Send is the composer's Enter, and its Send button. A /poll or
// /todo line is the draft of a card and is never sent as a message: Enter
// posts the card the preview shows. A line whose preview the person has not
// seen yet ("/poll" and Enter, or a line pasted and sent in one go) opens it
// and posts nothing.
func chatcmd003Send(m Model, local localStore, target, body string) bool {
	shown := chatcmd003Adopt(m, local, target)
	seen := shown.Open && shown.Target == target && shown.Conversation == m.SelectedID
	name, _, command := parseComposerCommand(body)
	name = strings.ToLower(name)
	if !command || (name != "poll" && name != "todo") {
		return false
	}
	if !chatcmd003Available(m) {
		local.update(func(u *localUI) { u.composerNotice = chatcmd003Text(m, "unavailable") })
		return true
	}
	value := body
	if !strings.ContainsAny(strings.TrimSpace(body), " \t\n") {
		// "/poll" and Enter chooses the command, as the list does.
		value = "/" + name + " "
		replaceComposerText(target, value, len(utf16.Encode([]rune(value))))
	}
	if !chatcmd003Follow(m, local, target, value) {
		// A card command this composer cannot run here; the registry answers it.
		return false
	}
	if state := local.get().chatcmd003; seen && state.Draft.Card.Kind == shown.Draft.Card.Kind && strings.TrimSpace(state.Draft.Raw) == strings.TrimSpace(shown.Draft.Raw) {
		chatcmd003Post(m, local)
	}
	return true
}

// chatcmd003Key gives an open preview its Escape from the composer's key
// handler: the preview closes and the composer is left empty. It reports
// whether the key was the preview's.
func chatcmd003Key(e ui.KeyboardEvent, m Model, local localStore, target string) bool {
	if e.GetKey() != "Escape" || composerIsComposing(e) {
		return false
	}
	state := chatcmd003Adopt(m, local, target)
	if !state.Open || state.Target != target || state.Conversation != m.SelectedID {
		return false
	}
	e.PreventDefault()
	e.StopPropagation()
	chatcmd003Action(m, local, "chatcmd003-cancel", "", "")
	return true
}

// chatcmd003Empty leaves the composer of a closed preview empty, with the
// caret at its start. The field's own input event files the empty draft where
// every other change of the text is filed (the tab's draft store and the
// client), so the text does not come back when the page is reloaded.
func chatcmd003Empty(m Model, state chatcmd003Preview) {
	replaceComposerText(state.Target, "", 0)
	setDOMValue(state.Target, "")
	switch {
	case state.Target == "chat-composer" && m.Callbacks.DraftChanged != nil:
		m.Callbacks.DraftChanged(state.Conversation, "")
	case state.Target == "thread-composer" && m.Callbacks.ThreadDraftChanged != nil:
		m.Callbacks.ThreadDraftChanged(state.Parent, "")
	}
	focusField(state.Target)
}

// chatcmd003Problem names what keeps a card from being posted, or "" when it
// can be. The preview says it in words; Post stays disabled until it is gone.
func chatcmd003Problem(c chat.Chatcmd002Card) string {
	if c.Validate() == nil {
		return ""
	}
	switch {
	case c.Poll != nil && len(c.Poll.Options) > 12:
		return "too-many"
	case c.Todo != nil && len(c.Todo.Items) > 30:
		return "too-many-items"
	case c.Todo != nil && len(c.Todo.Items) == 0:
		return "need-items"
	case c.Poll != nil && len(c.Poll.Options) < 2:
		return "need-options"
	}
	if len(c.Title) > 240 {
		return "too-long"
	}
	if c.Poll != nil {
		seen := map[string]bool{}
		for _, option := range c.Poll.Options {
			key := strings.ToLower(strings.TrimSpace(option.Text))
			if len(option.Text) > 100 {
				return "too-long"
			}
			if key == "" || seen[key] {
				return "need-options"
			}
			seen[key] = true
		}
	}
	if c.Todo != nil {
		for _, item := range c.Todo.Items {
			if len(item.Text) > 500 {
				return "too-long"
			}
			if strings.TrimSpace(item.Text) == "" {
				return "need-items"
			}
		}
	}
	return "parse"
}

// chatcmd003PostError is the line that says why a post was refused. The
// client's error says which refusal it was through Code; anything else is the
// server not answering.
func chatcmd003PostError(err error) string {
	var coded interface{ Code() string }
	if errors.As(err, &coded) {
		switch coded.Code() {
		case "permission_denied":
			return "post-denied"
		case "invalid_argument":
			return "post-refused"
		case "not_found":
			return "post-gone"
		}
	}
	return "post-error"
}

// chatcmd003Post posts the card the preview shows. It does nothing while the
// preview has nothing that can be posted; the preview already says why.
func chatcmd003Post(m Model, local localStore) {
	state := local.get().chatcmd003
	if !state.Open || state.Busy || m.Chatcmd002.Post == nil || state.Error == "parse" || strings.TrimSpace(state.Draft.Raw) == "" {
		return
	}
	// "In an hour" counts from the press, not from the last keystroke.
	card := chatcmd003Apply(m, chatcmd003CloneCard(state.Draft.Card), state.Settings)
	if chatcmd003Problem(card) != "" {
		return
	}
	local.update(func(u *localUI) {
		u.chatcmd003.Draft.Card, u.chatcmd003.Busy, u.chatcmd003.Posting, u.chatcmd003.Error = card, true, true, ""
	})
	m.Chatcmd002.Post(state.Conversation, state.Parent, card, func(err error) {
		current := local.get().chatcmd003
		if !current.Open || current.Generation != state.Generation {
			return
		}
		if err != nil {
			// The draft stays in the composer and the preview says why.
			local.update(func(u *localUI) {
				u.chatcmd003.Busy, u.chatcmd003.Posting, u.chatcmd003.Error = false, false, chatcmd003PostError(err)
			})
			return
		}
		local.update(func(u *localUI) {
			u.chatcmd003 = chatcmd003Preview{Touched: true, Generation: current.Generation + 1}
			u.commandMenu = composerCommandMenu{}
		})
		chatcmd003Empty(m, state)
		// The standing poll this card was moved from has its card now.
		if current.Standing && m.Chatcmd002.CloseChannelPoll != nil {
			m.Chatcmd002.CloseChannelPoll()
		}
	})
}

// chatcmd003AddLine is the line the add menu writes into a composer that holds
// value: the command word of kind and, after it, what the composer held. An
// ordinary line becomes the draft whole; a card command keeps its text and
// takes the kind chosen.
func chatcmd003AddLine(m Model, target, kind, value string) string {
	rest := strings.TrimSpace(value)
	if _, raw, ok := chatcmd003Line(m, target, rest); ok {
		rest = raw
	} else if name, args, command := parseComposerCommand(rest); command && args == "" && (strings.EqualFold(name, "poll") || strings.EqualFold(name, "todo")) {
		rest = ""
	}
	return "/" + kind + " " + rest
}

func chatcmd003Action(m Model, local localStore, action, id, extra string) bool {
	if action == "composer-add" && (extra == "poll" || extra == "todo") && chatcmd003Available(m) {
		closeComposerAddMenu(false)
		target := id
		if target == "" {
			target = "chat-composer"
		}
		// The menu writes the command word into the composer, so choosing from
		// the menu and typing the command are one way in and the text itself
		// says it is a card.
		value := chatcmd003AddLine(m, target, extra, domValue(target))
		replaceComposerText(target, value, len(utf16.Encode([]rune(value))))
		chatcmd003Follow(m, local, target, value)
		return true
	}
	if action == "chatbug057-move" {
		// The channel's standing poll becomes the draft of a card; posting it
		// closes the standing poll.
		target := id
		if target == "" {
			target = "chat-composer"
		}
		if chatbug057CanMove(m) {
			value := chatbug057MoveLine(m.ChannelPoll)
			replaceComposerText(target, value, len(utf16.Encode([]rune(value))))
			if chatcmd003Follow(m, local, target, value) {
				local.update(func(u *localUI) { u.chatcmd003.Standing = true })
			}
		}
		return true
	}
	if action != "command-pick" {
		// A press anywhere but a row of the "/" list closes it in the page at
		// once; the state follows in composerToolsClick.
		if open := local.get().commandMenu; open.Open {
			commandMenuShow(open.Target, nil, 0)
		}
	}
	if composerCommandAction(local, action, id, extra) {
		return true
	}
	if action == "poll-close" {
		// Ending the channel's poll closes its card: there is nothing left in it.
		if m.Chatcmd002.CloseChannelPoll != nil && m.ChannelTeam.CanPin && m.ChannelPoll.Question != "" && !m.ChannelPollPending && !m.ChannelPollLoading {
			m.Chatcmd002.CloseChannelPoll()
			local.update(func(u *localUI) { u.tray = "" })
			restoreChatLayerFocus("poll")
		}
		return true
	}
	if !strings.HasPrefix(action, "chatcmd003-") {
		return chatcmd002ActionLocal(m, local, action, id, extra)
	}
	state := chatcmd003Adopt(m, local, "chat-composer")
	if !state.Open || state.Conversation != m.SelectedID || (state.Busy && action != "chatcmd003-cancel") {
		return true
	}
	kind := state.Draft.Card.Kind
	switch action {
	case "chatcmd003-cancel":
		commandMenuShow(state.Target, nil, 0)
		local.update(func(u *localUI) {
			u.chatcmd003 = chatcmd003Preview{Touched: true, Generation: state.Generation + 1}
			u.commandMenu = composerCommandMenu{}
		})
		chatcmd003Empty(m, state)
	case "chatcmd003-original":
		// "Use what I typed", and back to the tidied wording.
		state.Typed = extra != "tidied"
		next := chatcmd003Draw(m, state, kind, state.Draft.Raw)
		local.update(func(u *localUI) { u.chatcmd003 = next })
		focusField(state.Target)
	case "chatcmd003-set":
		// One of the preview's controls: id names the setting, extra its value.
		settings := map[string]string{id: extra}
		for name, value := range state.Settings {
			if name != id {
				settings[name] = value
			}
		}
		state.Settings = settings
		next := chatcmd003Draw(m, state, kind, state.Draft.Raw)
		local.update(func(u *localUI) { u.chatcmd003 = next })
	case "chatcmd003-tidy":
		if m.Chatcmd002.Tidy == nil || state.Error == "parse" {
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
					draft.Card = chatcmd003Apply(m, draft.Card, state.Settings)
					u.chatcmd003.Draft = draft
				}
			})
		})
	case "chatcmd003-post":
		chatcmd003Post(m, local)
	}
	return true
}

// chatcmd003Switch is one of the preview's yes-or-no settings: a button that
// says what it turns on and whether it is on.
func chatcmd003Switch(m Model, name, label string, on, disabled bool) ui.Node {
	class, next := "chatcmd003-choice", "yes"
	if on {
		class, next = class+" selected", "no"
	}
	return html.Button(html.Props{ID: "chatcmd003-" + name, Class: class, Type: "button", Role: "switch", Disabled: disabled,
		Data: map[string]string{"action": "chatcmd003-set", "id": name, "extra": next},
		Aria: map[string]string{"checked": boolString(on)}, Text: chatcmd003Text(m, label)})
}

// chatcmd003Choice is one of the preview's settings with a few values: its
// label and one button for each value, the chosen one marked. The words of a
// value are the copy line named prefix and the value, or the entry of labels
// for a value the copy table does not hold (a typed date).
func chatcmd003Choice(m Model, name, label, current string, disabled bool, prefix string, labels map[string]string, options ...string) ui.Node {
	buttons := make([]ui.Node, 0, len(options))
	for _, option := range options {
		text := labels[option]
		if text == "" {
			text = chatcmd003Text(m, prefix+option)
		}
		class := "chatcmd003-choice"
		if option == current {
			class += " selected"
		}
		buttons = append(buttons, html.Button(html.Props{Class: class, Type: "button", Role: "radio", Disabled: disabled,
			Data: map[string]string{"action": "chatcmd003-set", "id": name, "extra": option},
			Aria: map[string]string{"checked": boolString(option == current)}, Text: text}))
	}
	labelID := "chatcmd003-" + name + "-label"
	return html.Div(html.Props{ID: "chatcmd003-" + name, Class: "chatcmd003-setting", Role: "radiogroup", Aria: map[string]string{"labelledby": labelID}},
		html.Span(html.Props{ID: labelID, Class: "chatcmd003-setting-label", Text: chatcmd003Text(m, label)}),
		html.Div(html.Props{Class: "chatcmd003-choices"}, buttons...))
}

// chatcmd003Settings is the row of controls under the card. Each is a button,
// so a choice reaches the preview on the press and the card above shows it.
func chatcmd003Settings(m Model, state chatcmd003Preview) ui.Node {
	var rows []ui.Node
	if p := state.Draft.Card.Poll; p != nil {
		closes, labels, presets := state.Settings["closes"], map[string]string{}, []string{"never", "hour", "today", "tomorrow", "week"}
		if typed := state.Draft.Original.Poll; typed != nil && typed.ClosesAt != nil {
			// A date typed in the text (closes=friday) is one more choice.
			labels["typed"] = chatcmd002Stamp(m, *typed.ClosesAt)
			presets = append(presets, "typed")
			if closes == "" {
				closes = "typed"
			}
		}
		if closes == "" {
			closes = "never"
		}
		rows = append(rows,
			html.Div(html.Props{Class: "chatcmd003-switches"},
				chatcmd003Switch(m, "multiple", "multiple", p.Multiple, state.Busy),
				chatcmd003Switch(m, "anonymous", "anonymous", p.Anonymous, state.Busy)),
			chatcmd003Choice(m, "results", "results", p.Results, state.Busy, "", nil, "always", "after-voting", "after-closing"),
			chatcmd003Choice(m, "closes", "closes", closes, state.Busy, "closes-", labels, presets...))
	}
	if t := state.Draft.Card.Todo; t != nil {
		rows = append(rows, chatcmd003Choice(m, "tick", "tick-policy", t.Tick, state.Busy, "", nil, "anyone", "assignee", "author"))
	}
	return html.Div(html.Props{Class: "chatcmd003-settings"}, rows...)
}

// chatcmd003Changes lists what the preview changed in what was typed, so
// nothing is altered out of sight: wording as the text before and after, and
// what was read out of the text (tasks split, an assignee, a date) in words.
func chatcmd003Changes(m Model, changes []chat.Chatcmd003Change) ui.Node {
	rows := make([]ui.Node, 0, len(changes))
	for _, change := range changes {
		switch {
		case change.Kind == chat.Chatcmd004ChangeSplit:
			rows = append(rows, html.Li(html.Props{Dir: "auto", Text: chatcmd002Fill(chatcmd003Text(m, "change-split"), "n", change.After)}))
		case change.Kind == chat.Chatcmd004ChangeAssignee:
			rows = append(rows, html.Li(html.Props{Dir: "auto", Text: chatcmd002Fill(chatcmd003Text(m, "change-assignee"), "task", change.Before, "name", change.After)}))
		case change.Kind == chat.Chatcmd004ChangeDue:
			rows = append(rows, html.Li(html.Props{Dir: "auto", Text: chatcmd002Fill(chatcmd003Text(m, "change-due"), "task", change.Before, "date", change.After)}))
		case change.After == "":
			rows = append(rows, html.Li(html.Props{Dir: "auto", Text: chatcmd002Fill(chatcmd003Text(m, "merged"), "text", change.Before)}))
		default:
			rows = append(rows, html.Li(html.Props{Dir: "auto"}, html.Tag("del", html.Props{Text: change.Before}), ui.Text(" → "), html.Tag("ins", html.Props{Text: change.After})))
		}
	}
	return html.Ul(html.Props{Class: "chatcmd003-changes"}, rows...)
}

func chatcmd003PreviewView(m Model, state chatcmd003Preview) ui.Node {
	if !state.Open || state.Conversation != m.SelectedID {
		return nil
	}
	kind := state.Draft.Card.Kind
	button := func(action, key, extra string, disabled bool) ui.Node {
		return html.Button(html.Props{Class: "button secondary small", Type: "button", Disabled: disabled, Data: map[string]string{"action": "chatcmd003-" + action, "extra": extra}, Text: chatcmd003Text(m, key)})
	}
	children := []ui.Node{html.H3(html.Props{ID: "chatcmd003-heading", Text: chatcmd003Text(m, kind+"-preview")})}
	empty := strings.TrimSpace(state.Draft.Raw) == ""
	problem := ""
	switch {
	case empty:
		// Nothing typed yet: how to write one, in the place the card will take.
		children = append(children, html.P(html.Props{ID: "chatcmd003-usage", Class: "chatcmd003-note", Dir: "auto", Text: chatcmd003Text(m, kind+"-usage")}))
	case state.Error == "parse":
		children = append(children, html.P(html.Props{ID: "chatcmd003-error", Role: "alert", Text: chatcmd003Text(m, "parse")}))
	default:
		problem = chatcmd003Problem(state.Draft.Card)
		children = append(children, Chatcmd002RenderCard(m, "", chat.Chatcmd002View{Card: state.Draft.Card, ResultsVisible: true}, true))
		if problem != "" {
			// A status, not an alert: it is true of most cards while they are typed.
			children = append(children, html.P(html.Props{ID: "chatcmd003-problem", Class: "chatcmd003-problem", Role: "status", Text: chatcmd003Text(m, problem)}))
		}
		wording := len(state.Draft.Changes) - len(chatcmd003Structural(state.Draft.Changes))
		if len(state.Draft.Changes) > 0 || state.Typed {
			count := chatcmd003Text(m, "change-one")
			switch {
			case state.Typed && len(state.Draft.Changes) == 0:
				count = chatcmd003Text(m, "as-typed")
			case len(state.Draft.Changes) != 1:
				count = chatcmd002Fill(chatcmd003Text(m, "changes"), "n", strconv.Itoa(len(state.Draft.Changes)))
			}
			row := []ui.Node{html.P(html.Props{Class: "chatcmd003-note", Role: "status", Text: count})}
			switch {
			case state.Typed:
				row = append(row, button("original", "tidied", "tidied", state.Busy))
			case wording > 0:
				row = append(row, button("original", "original", "typed", state.Busy))
			}
			children = append(children, html.Div(html.Props{Class: "chatcmd003-row"}, row...))
			if len(state.Draft.Changes) > 0 {
				children = append(children, chatcmd003Changes(m, state.Draft.Changes))
			}
		}
		for _, issue := range state.Draft.Issues {
			key := issue
			if issue == "assignee" {
				key = "ambiguous-assignee"
			}
			children = append(children, html.P(html.Props{Class: "chatcmd003-note", Role: "status", Text: chatcmd003Text(m, key)}))
		}
		children = append(children, chatcmd003Settings(m, state))
	}
	if note := chatbug057MoveNote(m, state); note != nil {
		children = append(children, note)
	}
	if state.Error != "" && state.Error != "parse" {
		// Why the last press did not do what it should: said beside the preview,
		// with the draft still in the composer.
		children = append(children, html.P(html.Props{ID: "chatcmd003-error", Role: "alert", Text: chatcmd003Text(m, state.Error)}))
	}
	if state.Busy {
		key := "working"
		if state.Posting {
			key = "posting"
		}
		children = append(children, html.P(html.Props{Class: "chatcmd003-note", Role: "status", Aria: map[string]string{"live": "polite"}, Text: chatcmd003Text(m, key)}))
	}
	ready := !empty && state.Error != "parse" && problem == ""
	if m.Chatcmd002.Tidy != nil && ready {
		children = append(children, html.Div(html.Props{Class: "chatcmd003-row"}, button("tidy", "tidy", "", state.Busy), html.P(html.Props{Class: "chatcmd003-note", Text: chatcmd003Text(m, "ai-notice") + " " + chatcmd003Text(m, "cost")})))
	}
	// Post is the default action: it is the filled button and Enter in the
	// composer presses it. It stays outside the scrolled body, so on a short
	// window Post and Cancel are in view whatever the preview holds.
	actions := html.Div(html.Props{Class: "chatcmd003-actions"},
		html.Button(html.Props{ID: "chatcmd003-post", Class: "button small", Type: "button", Disabled: !ready || state.Busy || m.Chatcmd002.Post == nil, Data: map[string]string{"action": "chatcmd003-post"}, Text: chatcmd003Text(m, kind+"-post")}),
		button("cancel", "cancel", "", false),
		html.Span(html.Props{Class: "chatcmd003-keys kbd-hint", Text: chatcmd003Text(m, "keys")}))
	return html.Section(html.Props{Class: "chatcmd003-preview", Role: "region", Dir: agentReplyDirection(m.Locale), Data: map[string]string{"kind": kind}, Aria: map[string]string{"labelledby": "chatcmd003-heading"}},
		html.Div(html.Props{Class: "chatcmd003-body"}, children...), actions)
}

// chatcmd003PreviewFor is the preview's place in the composer named by target.
// The place is in the page whether or not a preview is open: a child that
// comes and goes moves the text field behind it in its parent's list, and the
// field is then made again and loses its caret.
func chatcmd003PreviewFor(m Model, state chatcmd003Preview, target string) ui.Node {
	state = chatcmd003Effective(m, state, target)
	if state.Target != target || !state.Open || state.Conversation != m.SelectedID {
		return html.Div(html.Props{Class: "chatcmd003-slot"})
	}
	return html.Div(html.Props{Class: "chatcmd003-slot"}, chatcmd003PreviewView(m, state))
}

// Chatcmd003RenderPreview exposes the same card preview to command producers.
func Chatcmd003RenderPreview(m Model, draft chat.Chatcmd003Draft) ui.Node {
	return chatcmd003PreviewView(m, chatcmd003Preview{Open: true, Conversation: m.SelectedID, Draft: draft})
}
