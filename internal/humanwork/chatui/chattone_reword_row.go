package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// ChattoneRewordSetting is what applies to a conversation: the administrator's
// "Reword heated messages" and "Members may view messages as written".
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

func chattoneRewordPressed(label string, pressed bool, on ui.Handler, disabled bool) ui.Node {
	return html.Button(html.Props{Type: "button", Class: "button secondary chattone-reword-choice", Text: label, OnClick: on, Disabled: disabled || on.Value() == nil,
		Aria: map[string]string{"pressed": boolString(pressed)}})
}

func chattoneRewordGroup(id, label string, children ...ui.Node) ui.Node {
	return html.Div(html.Props{ID: id, Class: "chattone-reword-group", Role: "group", Aria: map[string]string{"label": label}}, children...)
}

// RenderChattoneRewordRow draws the row from what the server said. status is
// "", "saved" or "failed"; busy keeps the choices from being pressed twice.
func RenderChattoneRewordRow(locale string, v ChattoneRewordState, status string, busy bool, h chattoneRewordHandlers) ui.Node {
	if !chattoneRewordVisible(v) {
		return nil
	}
	t := func(key string) string { return ChattoneRewordText(locale, key) }
	children := []ui.Node{}
	if !v.ChoiceAllowed && !v.CanAdmin && v.Channel.Mode != "off" {
		children = append(children, html.P(html.Props{Class: "chattone-reword-note", Dir: agentReplyDirection(locale), Text: t("forced")}))
	}
	if v.ChoiceAllowed {
		general, channel := v.General, v.ForChannel
		pick := func(group string, current string, handlers []ui.Handler) []ui.Node {
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
			html.P(html.Props{Class: "chattone-reword-help", Dir: agentReplyDirection(locale), Text: t("show_help")}),
			chattoneRewordGroup("chattone-reword-general", t("everywhere"), pick("general", general, h.PersonGeneral[:])...),
			chattoneRewordGroup("chattone-reword-channel", t("here"), append(pick("channel", channel, h.PersonChannel[:2]),
				html.Button(html.Props{Type: "button", Class: "button secondary chattone-reword-choice", Text: t("general"), OnClick: h.PersonChannel[2], Disabled: busy || channel == "" || h.PersonChannel[2].Value() == nil}))...))
	}
	if v.CanAdmin {
		scope := func(id, title string, s ChattoneRewordSetting, mode [3]ui.Handler, view [2]ui.Handler, extra ...ui.Node) ui.Node {
			modeButtons := []ui.Node{}
			for i, m := range chattoneRewordModes {
				modeButtons = append(modeButtons, chattoneRewordPressed(t(m), s.Mode == m, mode[i], busy))
			}
			viewHelp := "yes_help"
			if !s.MembersMayViewOriginal {
				viewHelp = "no_help"
			}
			viewButtons := []ui.Node{chattoneRewordPressed(t("yes"), s.MembersMayViewOriginal, view[0], busy), chattoneRewordPressed(t("no"), !s.MembersMayViewOriginal, view[1], busy)}
			body := []ui.Node{
				html.H4(html.Props{Class: "chattone-reword-scope", Text: title}),
				chattoneRewordGroup(id+"-mode", t("mode"), modeButtons...),
				html.P(html.Props{Class: "chattone-reword-help", Dir: agentReplyDirection(locale), Text: t(s.Mode + "_help")}),
				chattoneRewordGroup(id+"-view", t("view"), viewButtons...),
				html.P(html.Props{Class: "chattone-reword-help", Dir: agentReplyDirection(locale), Text: t(viewHelp)}),
			}
			return html.Div(html.Props{Class: "chattone-reword-admin-scope", Data: map[string]string{"scope": id}}, append(body, extra...)...)
		}
		clear := []ui.Node{}
		if v.Channel.Override {
			clear = append(clear, html.P(html.Props{Class: "chattone-reword-help", Text: t("override")}),
				html.Button(html.Props{Type: "button", Class: "button secondary chattone-reword-choice", Text: t("inherit"), OnClick: h.ChannelClear, Disabled: busy || h.ChannelClear.Value() == nil}))
		}
		children = append(children,
			html.H4(html.Props{Class: "chattone-reword-admin", Text: t("admin")}),
			scope("workspace", t("workspace"), v.Workspace, h.WorkspaceMode, h.WorkspaceView),
			scope("channel", t("channel"), v.Channel, h.ChannelMode, h.ChannelView, clear...))
	}
	switch status {
	case "saved":
		children = append(children, html.P(html.Props{Class: "chattone-reword-status", Role: "status", Text: t("saved")}))
	case "failed":
		children = append(children, html.P(html.Props{Class: "chattone-reword-status", Role: "alert", Text: t("failed")}),
			html.Button(html.Props{Type: "button", Class: "button secondary", Text: t("retry"), OnClick: h.Retry, Disabled: h.Retry.Value() == nil}))
	default:
		children = append(children, html.P(html.Props{Class: "chattone-reword-status", Role: "status", Hidden: true}))
	}
	value := t(v.Channel.Mode)
	if v.Channel.Mode == "" {
		value = t("off")
	}
	return chatux002Row("heated-messages", t("title"), value, nil, html.Div(html.Props{Class: "chattone-reword", Dir: agentReplyDirection(locale)}, children...))
}

// ChattoneRewordStyles is the row's own rules; the buttons are the page's.
const ChattoneRewordStyles = `.chattone-reword{display:flex;flex-direction:column;gap:8px;min-width:0}.chattone-reword-group{display:flex;flex-wrap:wrap;gap:6px}.chattone-reword-choice{min-height:44px;min-width:44px;max-width:100%;overflow-wrap:anywhere}.chattone-reword-choice[aria-pressed="true"]{border-color:var(--accent);box-shadow:inset 0 0 0 1px var(--accent)}.chattone-reword-help,.chattone-reword-note{margin:0;font-size:.8125rem;color:var(--hcm-color-text-muted);overflow-wrap:anywhere}.chattone-reword-admin,.chattone-reword-scope{margin:8px 0 0;font-size:.9rem}.chattone-reword-status[hidden]{display:none}`

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
	send := func(path string, body map[string]any) {
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
					next.View, next.Status = view, "saved"
				}
				state.Set(next)
			})
		})
	}
	var h chattoneRewordHandlers
	for i, tone := range chattoneRewordTones {
		tone := tone
		h.PersonGeneral[i] = ui.UseEvent(func() { send("/reword/choice", map[string]any{"scope": "general", "tone": tone}) })
	}
	for i := 0; i < 3; i++ {
		i := i
		if i < 2 {
			tone := chattoneRewordTones[i]
			h.PersonChannel[i] = ui.UseEvent(func() { send("/reword/choice", map[string]any{"scope": "channel", "tone": tone}) })
		} else {
			h.PersonChannel[i] = ui.UseEvent(func() { send("/reword/choice", map[string]any{"scope": "channel", "clear": true}) })
		}
	}
	admin := func(scope string, mode string, view bool) {
		send("/reword/admin", map[string]any{"scope": scope, "mode": mode, "members_may_view_original": view})
	}
	for i, mode := range chattoneRewordModes {
		mode := mode
		h.WorkspaceMode[i] = ui.UseEvent(func() { admin("workspace", mode, state.Get().View.Workspace.MembersMayViewOriginal) })
	}
	for i := 0; i < 2; i++ {
		view := i == 0
		h.WorkspaceView[i] = ui.UseEvent(func() { admin("workspace", state.Get().View.Workspace.Mode, view) })
	}
	for i, mode := range chattoneRewordModes {
		mode := mode
		h.ChannelMode[i] = ui.UseEvent(func() { admin("channel", mode, state.Get().View.Channel.MembersMayViewOriginal) })
	}
	for i := 0; i < 2; i++ {
		view := i == 0
		h.ChannelView[i] = ui.UseEvent(func() { admin("channel", state.Get().View.Channel.Mode, view) })
	}
	h.ChannelClear = ui.UseEvent(func() { send("/reword/admin", map[string]any{"scope": "channel", "clear": true}) })
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
