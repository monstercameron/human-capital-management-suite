package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// ChattoneRewordSetting is what applies to a conversation: the administrator's
// "Reword heated messages" and "Members may read messages as written".
type ChattoneRewordSetting struct {
	Mode                   string `json:"mode"`
	MembersMayViewOriginal bool   `json:"members_may_view_original"`
	Override               bool   `json:"override"`
}

// ChattoneRewordState is the server's answer for one conversation.
type ChattoneRewordState struct {
	Available     bool                  `json:"available"`
	Workspace     ChattoneRewordSetting `json:"workspace"`
	Channel       ChattoneRewordSetting `json:"channel"`
	CanAdmin      bool                  `json:"can_administer"`
	ChoiceAllowed bool                  `json:"choice_allowed"`
	General       string                `json:"general_choice"`
	ForChannel    string                `json:"channel_choice"`
}

// chattoneRewordVisible: the row is drawn for an administrator, and for anyone
// where rewording is on or offered; a member in a workspace with rewording off
// has nothing to choose and nothing to be told.
func chattoneRewordVisible(v ChattoneRewordState) bool {
	return v.Available && (v.CanAdmin || v.Channel.Mode == "offered" || v.Channel.Mode == "on")
}

// chattoneRewordHandlers are the row's presses. Every one exists on every
// render (hooks are positional); a press whose control is not drawn is never
// reached.
type chattoneRewordHandlers struct {
	PersonGeneral [2]ui.Handler // as-written, reworded
	PersonChannel [3]ui.Handler // as-written, reworded, clear
	WorkspaceMode [3]ui.Handler // off, offered, on
	WorkspaceView [2]ui.Handler // yes, no
	ChannelMode   [3]ui.Handler
	ChannelView   [2]ui.Handler
	ChannelClear  ui.Handler
	Retry         ui.Handler
}

var (
	chattoneRewordModes = [3]string{"off", "offered", "on"}
	chattoneRewordTones = [2]string{"as-written", "reworded"}
)

// chattoneRewordPressed is one choice of a segmented control: the page's own
// segmented button (the one Saved and Moderation use), pressed when it is the
// current value.
func chattoneRewordPressed(label string, pressed bool, on ui.Handler, disabled bool) ui.Node {
	return html.Button(html.Props{Type: "button", Class: "chatsave-seg-button chattone-reword-choice", Text: label, OnClick: on, Disabled: disabled || on.Value() == nil,
		Aria: map[string]string{"pressed": boolString(pressed)}})
}

// chattoneRewordGroup is a segmented control named by the visible label of its
// row, so a screen reader hears the same question the page shows.
func chattoneRewordGroup(id, labelID, helpID string, children ...ui.Node) ui.Node {
	aria := map[string]string{"labelledby": labelID}
	if helpID != "" {
		aria["describedby"] = helpID
	}
	return html.Div(html.Props{ID: id, Class: "chatsave-seg chattone-reword-seg", Role: "group", Aria: aria}, children...)
}

// chattoneRewordSwitch is the row-end switch of a yes-or-no setting: the track
// the channel filters use, on a button, so the page's state alone decides how it
// reads and a failed save leaves it where it was.
func chattoneRewordSwitch(id, labelID, helpID string, on bool, press ui.Handler, disabled bool) ui.Node {
	return html.Button(html.Props{ID: id, Type: "button", Role: "switch", Class: "chatmod-switch chattone-reword-switch", OnClick: press, Disabled: disabled || press.Value() == nil,
		Aria: map[string]string{"checked": boolString(on), "labelledby": labelID, "describedby": helpID}},
		html.Span(html.Props{Class: "chatmod-switch-track", Aria: map[string]string{"hidden": "true"}}))
}

// chattoneRewordRow is one setting of the reword block, drawn as the rows above
// it in Chat preferences are: the label first, its control at the end of the row
// (when it has one that fits there), the saved tick in the slot after it, one
// line of help under the label, and a segmented control under that when the
// choice has more than two values. saved is the key of the row that was just
// saved, "" when none was.
func chattoneRewordRow(locale, key, label, help, saved string, end ui.Node, below ...ui.Node) ui.Node {
	head := []ui.Node{html.Span(html.Props{ID: chattoneRewordLabelID(key), Class: "chat-prefs-title", Dir: "auto", Text: label})}
	if end != nil {
		head = append(head, end)
	}
	// The slot is always drawn, so the control does not move when the tick comes.
	var tick []ui.Node
	if saved == key {
		tick = append(tick, html.Span(html.Props{Class: "chatux031-tick", Text: "✓"}))
	}
	head = append(head, html.Span(html.Props{Class: "chatux031-end", Aria: map[string]string{"hidden": "true"}}, tick...))
	nodes := []ui.Node{html.Div(html.Props{Class: "chat-prefs-head chattone-reword-head"}, head...)}
	if help != "" {
		nodes = append(nodes, html.P(html.Props{ID: chattoneRewordHelpID(key), Class: "chattone-reword-help", Dir: agentReplyDirection(locale), Text: help}))
	}
	nodes = append(nodes, below...)
	return html.Div(html.Props{Class: "chattone-reword-row", Dir: agentReplyDirection(locale), Data: map[string]string{"reword-row": key}}, nodes...)
}

func chattoneRewordLabelID(row string) string { return "chattone-reword-" + row + "-label" }
func chattoneRewordHelpID(row string) string  { return "chattone-reword-" + row + "-help" }

// RenderChattoneRewordRow draws the row from what the server said. status is
// "", "saved", "saved:<row>" (the row whose choice was just saved) or "failed";
// busy keeps the choices from being pressed twice.
func RenderChattoneRewordRow(locale string, v ChattoneRewordState, status string, busy bool, h chattoneRewordHandlers) ui.Node {
	if !chattoneRewordVisible(v) {
		return nil
	}
	t := func(key string) string { return ChattoneRewordText(locale, key) }
	state, saved, _ := strings.Cut(status, ":")
	children := []ui.Node{}
	if !v.ChoiceAllowed && !v.CanAdmin && v.Channel.Mode != "off" {
		children = append(children, html.P(html.Props{Class: "chattone-reword-note", Dir: agentReplyDirection(locale), Text: t("forced")}))
	}
	if v.ChoiceAllowed {
		general, channel := v.General, v.ForChannel
		pick := func(current string, handlers []ui.Handler) []ui.Node {
			out := []ui.Node{}
			for i, tone := range chattoneRewordTones {
				label := t("written")
				if tone == "reworded" {
					label = t("reworded")
				}
				out = append(out, chattoneRewordPressed(label, current == tone, handlers[i], busy))
			}
			return out
		}
		children = append(children,
			html.P(html.Props{Class: "chattone-reword-help chattone-reword-intro", Dir: agentReplyDirection(locale), Text: t("show_help")}),
			chattoneRewordRow(locale, "general", t("everywhere"), "", saved, nil,
				chattoneRewordGroup("chattone-reword-general", chattoneRewordLabelID("general"), "", pick(general, h.PersonGeneral[:])...)),
			chattoneRewordRow(locale, "channel", t("here"), "", saved, nil,
				chattoneRewordGroup("chattone-reword-channel", chattoneRewordLabelID("channel"), "", append(pick(channel, h.PersonChannel[:2]),
					html.Button(html.Props{Type: "button", Class: "chatsave-seg-button chattone-reword-choice", Text: t("general"), OnClick: h.PersonChannel[2], Disabled: busy || channel == "" || h.PersonChannel[2].Value() == nil}))...)))
	}
	if v.CanAdmin {
		scope := func(id, title string, s ChattoneRewordSetting, mode [3]ui.Handler, view [2]ui.Handler, extra ...ui.Node) ui.Node {
			modeKey, viewKey := id+"-mode", id+"-view"
			modeButtons := []ui.Node{}
			for i, m := range chattoneRewordModes {
				modeButtons = append(modeButtons, chattoneRewordPressed(t(m), s.Mode == m, mode[i], busy))
			}
			viewHelp := "yes_help"
			if !s.MembersMayViewOriginal {
				viewHelp = "no_help"
			}
			// One press flips the switch: it asks for the other answer.
			press := view[1]
			if !s.MembersMayViewOriginal {
				press = view[0]
			}
			body := []ui.Node{
				html.H5(html.Props{Class: "chattone-reword-scope", Dir: "auto", Text: title}),
				chattoneRewordRow(locale, modeKey, t("mode"), t(s.Mode+"_help"), saved, nil,
					chattoneRewordGroup(modeKey, chattoneRewordLabelID(modeKey), chattoneRewordHelpID(modeKey), modeButtons...)),
				chattoneRewordRow(locale, viewKey, t("view"), t(viewHelp), saved,
					chattoneRewordSwitch(viewKey, chattoneRewordLabelID(viewKey), chattoneRewordHelpID(viewKey), s.MembersMayViewOriginal, press, busy)),
			}
			return html.Div(html.Props{Class: "chattone-reword-admin-scope", Data: map[string]string{"scope": id}}, append(body, extra...)...)
		}
		clear := []ui.Node{}
		if v.Channel.Override {
			clear = append(clear, html.Div(html.Props{Class: "chattone-reword-override"},
				html.P(html.Props{Class: "chattone-reword-help", Dir: agentReplyDirection(locale), Text: t("override")}),
				html.Button(html.Props{Type: "button", Class: "button secondary small chattone-reword-inherit", Text: t("inherit"), OnClick: h.ChannelClear, Disabled: busy || h.ChannelClear.Value() == nil})))
		}
		children = append(children,
			html.H4(html.Props{Class: "chattone-reword-admin", Dir: "auto", Text: t("admin")}),
			scope("workspace", t("workspace"), v.Workspace, h.WorkspaceMode, h.WorkspaceView),
			scope("channel", t("channel"), v.Channel, h.ChannelMode, h.ChannelView, clear...))
	}
	switch state {
	case "saved":
		// The row's own tick shows the save; this line is for a screen reader.
		children = append(children, html.P(html.Props{Class: "chattone-reword-status", Role: "status", Data: map[string]string{"state": "saved"}, Text: t("saved")}))
	case "failed":
		children = append(children, html.P(html.Props{Class: "chattone-reword-status", Role: "alert", Data: map[string]string{"state": "error"}, Text: t("failed")}),
			html.Button(html.Props{Type: "button", Class: "button secondary small chattone-reword-retry", Text: t("retry"), OnClick: h.Retry, Disabled: h.Retry.Value() == nil}))
	default:
		children = append(children, html.P(html.Props{Class: "chattone-reword-status", Role: "status", Hidden: true}))
	}
	value := t(v.Channel.Mode)
	if v.Channel.Mode == "" {
		value = t("off")
	}
	return chatux002Row("heated-messages", t("title"), value, nil, html.Div(html.Props{Class: "chattone-reword", Dir: agentReplyDirection(locale)}, children...))
}

// ChattoneRewordStyles is the row's base rule; how the rows are laid out is
// ChatUX048Styles, which comes after the preferences styles it builds on.
const ChattoneRewordStyles = `.chattone-reword{min-width:0}`

type chattoneRewordProps struct{ Locale, Conversation string }

type chattoneRewordLocal struct {
	View       ChattoneRewordState
	Busy       bool
	Status     string
	Unprovided bool
}

// chattoneRewordComponent is the row with its own state: it asks the server
// once when it appears (and when the conversation changes), keeps the last good
// answer when a later one fails, and sends each press to the server and takes
// the new answer.
func chattoneRewordComponent(p chattoneRewordProps) ui.Node {
	state := ui.UseState(chattoneRewordLocal{})
	ui.UseEffectOf(func() func() {
		return chattoneRewordLoad(p.Conversation, func(view ChattoneRewordState, err error) {
			ui.PostAsync(func() {
				current := state.Get()
				if err == nil {
					current.View, current.Unprovided = view, false
				} else if chattoneRewordNotAvailable(err) {
					current.Unprovided = true
				}
				state.Set(current)
			})
		})
	}, struct{ Room string }{p.Conversation})
	// send saves one choice; row names the row it belongs to, so the tick appears
	// beside the choice that was saved.
	send := func(row, path string, body map[string]any) {
		current := state.Get()
		if current.Busy {
			return
		}
		current.Busy, current.Status = true, ""
		state.Set(current)
		body["conversation_id"] = p.Conversation
		chattoneRewordSend(path, body, func(view ChattoneRewordState, err error) {
			ui.PostAsync(func() {
				next := state.Get()
				next.Busy = false
				if err != nil {
					// The last good answer stays; the person is told and may try again.
					next.Status = "failed"
				} else {
					next.View, next.Status = view, "saved:"+row
				}
				state.Set(next)
			})
		})
	}
	var h chattoneRewordHandlers
	for i, tone := range chattoneRewordTones {
		tone := tone
		h.PersonGeneral[i] = ui.UseEvent(func() { send("general", "/reword/choice", map[string]any{"scope": "general", "tone": tone}) })
	}
	for i := 0; i < 3; i++ {
		i := i
		if i < 2 {
			tone := chattoneRewordTones[i]
			h.PersonChannel[i] = ui.UseEvent(func() { send("channel", "/reword/choice", map[string]any{"scope": "channel", "tone": tone}) })
		} else {
			h.PersonChannel[i] = ui.UseEvent(func() { send("channel", "/reword/choice", map[string]any{"scope": "channel", "clear": true}) })
		}
	}
	admin := func(scope, row string, mode string, view bool) {
		send(scope+"-"+row, "/reword/admin", map[string]any{"scope": scope, "mode": mode, "members_may_view_original": view})
	}
	for i, mode := range chattoneRewordModes {
		mode := mode
		h.WorkspaceMode[i] = ui.UseEvent(func() { admin("workspace", "mode", mode, state.Get().View.Workspace.MembersMayViewOriginal) })
	}
	for i := 0; i < 2; i++ {
		view := i == 0
		h.WorkspaceView[i] = ui.UseEvent(func() { admin("workspace", "view", state.Get().View.Workspace.Mode, view) })
	}
	for i, mode := range chattoneRewordModes {
		mode := mode
		h.ChannelMode[i] = ui.UseEvent(func() { admin("channel", "mode", mode, state.Get().View.Channel.MembersMayViewOriginal) })
	}
	for i := 0; i < 2; i++ {
		view := i == 0
		h.ChannelView[i] = ui.UseEvent(func() { admin("channel", "view", state.Get().View.Channel.Mode, view) })
	}
	h.ChannelClear = ui.UseEvent(func() { send("channel-mode", "/reword/admin", map[string]any{"scope": "channel", "clear": true}) })
	h.Retry = ui.UseEvent(func() {
		current := state.Get()
		current.Status = ""
		state.Set(current)
	})
	current := state.Get()
	if current.Unprovided {
		return nil
	}
	return RenderChattoneRewordRow(p.Locale, current.View, current.Status, current.Busy, h)
}
