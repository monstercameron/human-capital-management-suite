package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// ChatmapWorkspaceSettingsPanel loads the workspace's location settings and binds
// the forms to the server, for the Chat settings page. Every save is followed by
// a fresh read, so the page shows what the server kept and never what it hoped.
func ChatmapWorkspaceSettingsPanel(locale string) ui.Node {
	return ui.CreateElement(chatmapAdminPanel, chatmapAdminProps{Locale: locale})
}

type chatmapAdminProps struct{ Locale string }

func chatmapAdminPanel(props chatmapAdminProps) ui.Node {
	state := ui.UseState(ChatmapAdminModel{Locale: props.Locale})
	apply := func(data ChatmapAdminData, status int, err error, saved bool, field string) {
		current := state.Get()
		current.Loading, current.Invalid = false, false
		switch {
		case err == nil:
			current.Data, current.Loaded, current.Failed, current.Denied, current.Saved = data, true, false, false, saved
			current.SignedOut, current.SavedField = false, field
		case status == 403:
			current.Denied, current.Loaded, current.Failed = true, true, false
		default:
			// The last good settings stay on show; the failure line offers Try again.
			current.Failed, current.Saved = true, false
			current.SignedOut = status == 401
		}
		state.Set(current)
	}
	read := func(saved bool, field string) {
		chatmapAdminCall("settings", nil, func(data ChatmapAdminData, status int, err error) { apply(data, status, err, saved, field) })
	}
	ui.UseEffectOf(func() func() {
		return chatmapAdminCall("settings", nil, func(data ChatmapAdminData, status int, err error) { apply(data, status, err, false, "") })
	}, struct{ Page string }{"chatmap-admin"})
	// save writes, then reads the whole settings again.
	save := func(action string, body any, field string) {
		current := state.Get()
		current.Loading, current.Failed, current.Saved, current.Invalid = true, false, false, false
		state.Set(current)
		chatmapAdminCall(action, body, func(_ ChatmapAdminData, status int, err error) {
			if err != nil {
				apply(ChatmapAdminData{}, status, err, false, "")
				return
			}
			read(true, field)
		})
	}
	savePolicy := ui.UseEvent(func(event ui.Event) {
		event.PreventDefault()
		body, err := chatmapAdminReadPolicy(event)
		if err != nil {
			apply(ChatmapAdminData{}, 0, err, false, "")
			return
		}
		field := chatmapAdminChangedField(event)
		if field == "" {
			field = "sharing"
		}
		save("setpolicy", body, field)
	})
	saveCountry := ui.UseEvent(func(event ui.Event) {
		event.PreventDefault()
		body, err := chatmapAdminReadCountry(event)
		if err != nil {
			current := state.Get()
			current.Invalid, current.Saved = true, false
			state.Set(current)
			return
		}
		save("setjurisdiction", body, "country")
	})
	// Try again reads the settings once more, keeping what is on show meanwhile.
	retry := ui.UseEvent(func() {
		current := state.Get()
		current.Loading, current.Failed = true, false
		state.Set(current)
		read(false, "")
	})
	model := state.Get()
	model.SavePolicy, model.SaveCountry, model.Retry = savePolicy, saveCountry, retry
	return ChatmapWorkspaceSettings(model)
}
