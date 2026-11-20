package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// ChatHoldWorkspaceSettingsPanel loads the workspace's legal holds and binds the
// place and release forms to the server, for the Chat settings page (issue
// 3779). Every change is followed by a fresh read, so the page shows what the
// server kept and never what it hoped.
func ChatHoldWorkspaceSettingsPanel(locale string) ui.Node {
	return ui.CreateElement(chatHoldPanel, chatHoldProps{Locale: locale})
}

type chatHoldProps struct{ Locale string }

// chatHoldReadAnswered applies the answer to a read of the page.
func chatHoldReadAnswered(current ChatHoldModel, data ChatHoldData, status int, err error, notice string) ChatHoldModel {
	current.Loading = false
	switch {
	case err == nil:
		current.Data, current.Loaded, current.Failed, current.SignedOut = data, true, false, false
		current.Denied, current.Unavailable, current.Notice = false, false, notice
	case status == 403:
		current.Denied, current.Loaded, current.Failed = true, true, false
	case status == 503 && !current.Loaded:
		current.Unavailable, current.Loaded = true, true
	default:
		// The holds on show stay; the failure line offers Try again.
		current.Failed, current.Notice = true, ""
		current.SignedOut = status == 401
	}
	return current
}

// chatHoldRefusal explains a refusal the administrator can fix: on the place
// form ("invalid" or "gone") or on one hold's release form. It is empty when
// the change failed for another reason.
func chatHoldRefusal(action string, status int) string {
	switch {
	case action == "place" && status == 400:
		return "invalid"
	case action == "place" && status == 404:
		return "gone"
	case action == "release" && status == 400:
		return "release_missing"
	case action == "release" && (status == 404 || status == 409):
		return "release_gone"
	}
	return ""
}

func chatHoldPanel(props chatHoldProps) ui.Node {
	state := ui.UseState(ChatHoldModel{Locale: props.Locale})
	read := func(notice string) {
		chatHoldCall("list", nil, func(data ChatHoldData, status int, err error) {
			state.Set(chatHoldReadAnswered(state.Get(), data, status, err, notice))
		})
	}
	ui.UseEffectOf(func() func() {
		return chatHoldCall("list", nil, func(data ChatHoldData, status int, err error) {
			state.Set(chatHoldReadAnswered(state.Get(), data, status, err, ""))
		})
	}, struct{ Page string }{"chat-holds"})
	// change writes, then reads the whole page again.
	change := func(action, holdID string, body any, notice string) {
		current := state.Get()
		current.Loading, current.Failed, current.Notice, current.FormError, current.ReleaseError, current.ReleaseID = true, false, "", "", "", ""
		state.Set(current)
		chatHoldCall(action, body, func(_ ChatHoldData, status int, err error) {
			if err != nil {
				next := state.Get()
				if refusal := chatHoldRefusal(action, status); refusal != "" {
					// The server turned the form away: the form says why, and
					// the holds on show stay as they were.
					next.Loading = false
					if action == "place" {
						next.FormError = refusal
					} else {
						next.ReleaseError, next.ReleaseID = refusal, holdID
					}
					state.Set(next)
					return
				}
				state.Set(chatHoldReadAnswered(next, ChatHoldData{}, status, err, ""))
				return
			}
			read(notice)
		})
	}
	place := ui.UseEvent(func(event ui.Event) {
		event.PreventDefault()
		fields := chatHoldReadForm(event)
		current := state.Get()
		switch {
		case strings.TrimSpace(fields["conversation"]) == "":
			current.FormError, current.Notice = "missing", ""
			state.Set(current)
		case strings.TrimSpace(fields["reason"]) == "":
			current.FormError, current.Notice = "invalid", ""
			state.Set(current)
		default:
			change("place", "", map[string]string{"conversation_id": fields["conversation"], "matter": strings.TrimSpace(fields["matter"]), "reason": strings.TrimSpace(fields["reason"])}, "placed")
		}
	})
	release := ui.UseEvent(func(event ui.Event) {
		event.PreventDefault()
		fields := chatHoldReadForm(event)
		id := strings.TrimSpace(fields["id"])
		if id == "" {
			return
		}
		if strings.TrimSpace(fields["reason"]) == "" {
			current := state.Get()
			current.ReleaseError, current.ReleaseID, current.Notice = "release_missing", id, ""
			state.Set(current)
			return
		}
		change("release", id, map[string]string{"id": id, "reason": strings.TrimSpace(fields["reason"])}, "released")
	})
	retry := ui.UseEvent(func() {
		current := state.Get()
		current.Loading, current.Failed = true, false
		state.Set(current)
		read("")
	})
	model := state.Get()
	model.Place, model.Release, model.Retry = place, release, retry
	return ChatHoldSettings(model)
}
