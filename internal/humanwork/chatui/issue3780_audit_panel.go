package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// ChatAuditWorkspaceSettingsPanel loads the workspace's audit log and binds the
// filter form, Show more and Download CSV to the server, for the Chat settings
// page (#3780). It shows what the server returned and nothing it guessed.
func ChatAuditWorkspaceSettingsPanel(locale string) ui.Node {
	return ui.CreateElement(chatAuditPanel, chatAuditProps{Locale: locale})
}

type chatAuditProps struct{ Locale string }

// chatAuditAnswered applies the answer to a read. append adds the page to the
// entries on show (Show more); otherwise it replaces them.
func chatAuditAnswered(current ChatAuditModel, data ChatAuditData, status int, err error, filter ChatAuditFilter, appendPage bool) ChatAuditModel {
	current.Loading, current.LoadingMore = false, false
	switch {
	case err == nil:
		if appendPage {
			data.Entries = append(append([]ChatAuditEntry(nil), current.Data.Entries...), data.Entries...)
		}
		current.Data, current.Filter, current.Loaded = data, filter, true
		current.Failed, current.MoreFailed, current.SignedOut = false, false, false
	case status == 403:
		current.Denied, current.Loaded, current.Failed = true, true, false
	case status == 503 && !current.Loaded:
		current.Unavailable, current.Loaded = true, true
	case appendPage:
		// The entries on show stay; only the extra page failed.
		current.MoreFailed = true
	default:
		current.Failed = true
		current.SignedOut = status == 401
	}
	return current
}

func chatAuditPanel(props chatAuditProps) ui.Node {
	state := ui.UseState(ChatAuditModel{Locale: props.Locale})
	read := func(filter ChatAuditFilter, cursor string, appendPage bool) {
		current := state.Get()
		if appendPage {
			current.LoadingMore, current.MoreFailed = true, false
		} else {
			current.Loading, current.Failed = true, false
		}
		current.Exported, current.ExportFailed = false, false
		state.Set(current)
		chatAuditCall("list", chatAuditBody(filter, cursor), func(data ChatAuditData, status int, err error) {
			state.Set(chatAuditAnswered(state.Get(), data, status, err, filter, appendPage))
		})
	}
	ui.UseEffectOf(func() func() {
		return chatAuditCall("list", chatAuditBody(ChatAuditFilter{}, ""), func(data ChatAuditData, status int, err error) {
			state.Set(chatAuditAnswered(state.Get(), data, status, err, ChatAuditFilter{}, false))
		})
	}, struct{ Page string }{"chat-audit"})
	apply := ui.UseEvent(func(event ui.Event) {
		event.PreventDefault()
		read(chatAuditFilterClean(chatAuditReadFilter(event)), "", false)
	})
	clear := ui.UseEvent(func() {
		chatAuditResetForm()
		read(ChatAuditFilter{}, "", false)
	})
	more := ui.UseEvent(func() {
		current := state.Get()
		read(current.Filter, current.Data.NextCursor, true)
	})
	retry := ui.UseEvent(func() { read(state.Get().Filter, "", false) })
	export := ui.UseEvent(func() {
		current := state.Get()
		current.Exporting, current.Exported, current.ExportFailed = true, false, false
		state.Set(current)
		chatAuditExport(current.Filter, func(status int, err error) {
			next := state.Get()
			next.Exporting = false
			next.Exported, next.ExportFailed = err == nil, err != nil
			if status == 403 {
				next.Denied = true
			}
			state.Set(next)
		})
	})
	model := state.Get()
	model.Apply, model.Clear, model.More, model.Export, model.Retry = apply, clear, more, export, retry
	return ChatAuditLogSettings(model)
}

// chatAuditBody is the list request for a filter and a cursor.
func chatAuditBody(f ChatAuditFilter, cursor string) map[string]any {
	return map[string]any{"Actor": f.Actor, "Action": f.Action, "Conversation": f.Conversation, "FromDate": f.FromDate, "ToDate": f.ToDate, "Cursor": cursor}
}
