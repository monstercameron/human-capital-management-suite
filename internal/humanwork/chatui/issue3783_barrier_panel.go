package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// ChatBarrierWorkspaceSettingsPanel loads the workspace's information barriers
// and binds the add and remove forms to the server, for the Chat settings page
// (issue 3783). Every change is followed by a fresh read, so the page shows what
// the server kept and never what it hoped.
func ChatBarrierWorkspaceSettingsPanel(locale string) ui.Node {
	return ui.CreateElement(chatBarrierPanel, chatBarrierProps{Locale: locale})
}

type chatBarrierProps struct{ Locale string }

// chatBarrierReadAnswered applies the answer to a read of the page.
func chatBarrierReadAnswered(current ChatBarrierModel, data ChatBarrierData, status int, err error, notice string) ChatBarrierModel {
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
		// The barriers on show stay; the failure line offers Try again.
		current.Failed, current.Notice = true, ""
		current.SignedOut = status == 401
	}
	return current
}

// chatBarrierChangeRefusal is the add form's explanation for a refusal the
// administrator can fix, or empty when the change failed for another reason.
func chatBarrierChangeRefusal(action string, status int, code string) string {
	if action != "add" {
		return ""
	}
	switch {
	case status == 400 && code == "unknown_group":
		return "unknown"
	case status == 400:
		return "invalid"
	case status == 409 && code == "already_exists":
		return "duplicate"
	case status == 409:
		return "limit"
	}
	return ""
}

func chatBarrierPanel(props chatBarrierProps) ui.Node {
	state := ui.UseState(ChatBarrierModel{Locale: props.Locale})
	read := func(notice string) {
		chatBarrierCall("list", nil, func(data ChatBarrierData, _ string, status int, err error) {
			state.Set(chatBarrierReadAnswered(state.Get(), data, status, err, notice))
		})
	}
	ui.UseEffectOf(func() func() {
		return chatBarrierCall("list", nil, func(data ChatBarrierData, _ string, status int, err error) {
			state.Set(chatBarrierReadAnswered(state.Get(), data, status, err, ""))
		})
	}, struct{ Page string }{"chat-barriers"})
	// change writes, then reads the whole page again.
	change := func(action string, body any, notice string) {
		current := state.Get()
		current.Loading, current.Failed, current.Notice, current.FormError = true, false, "", ""
		state.Set(current)
		chatBarrierCall(action, body, func(_ ChatBarrierData, code string, status int, err error) {
			if err != nil {
				next := state.Get()
				if refusal := chatBarrierChangeRefusal(action, status, code); refusal != "" {
					// The server turned the form away: the form says why, and
					// the barriers on show stay as they were.
					next.Loading, next.FormError = false, refusal
					state.Set(next)
					return
				}
				state.Set(chatBarrierReadAnswered(next, ChatBarrierData{}, status, err, ""))
				return
			}
			read(notice)
		})
	}
	add := ui.UseEvent(func(event ui.Event) {
		event.PreventDefault()
		body, err := chatBarrierReadAdd(event)
		if err != nil {
			current := state.Get()
			current.FormError, current.Notice = "invalid", ""
			state.Set(current)
			return
		}
		change("add", body, "added")
	})
	remove := ui.UseEvent(func(event ui.Event) {
		event.PreventDefault()
		body, err := chatBarrierReadRemove(event)
		if err != nil {
			return
		}
		change("remove", body, "removed")
	})
	retry := ui.UseEvent(func() {
		current := state.Get()
		current.Loading, current.Failed = true, false
		state.Set(current)
		read("")
	})
	model := state.Get()
	model.Add, model.Remove, model.Retry = add, remove, retry
	return ChatBarrierSettings(model)
}
