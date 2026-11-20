package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// ChatRetentionPreviewWorkspaceSettingsPanel binds the preview button to the
// server for the Chat settings page (#3778). It shows what the server answered
// and nothing it guessed, and asks only when the administrator presses Preview.
func ChatRetentionPreviewWorkspaceSettingsPanel(locale string) ui.Node {
	return ui.CreateElement(chatRetentionPreviewPanel, chatRetentionPreviewProps{Locale: locale})
}

type chatRetentionPreviewProps struct{ Locale string }

// chatRetentionPreviewAnswered applies the answer to a read.
func chatRetentionPreviewAnswered(current ChatRetentionPreviewModel, data ChatRetentionPreviewData, status int, err error) ChatRetentionPreviewModel {
	current.Loading = false
	switch {
	case err == nil:
		current.Data, current.Loaded = data, true
		current.Failed, current.SignedOut = false, false
	case status == 403:
		current.Denied, current.Loaded, current.Failed = true, true, false
	case status == 503 && !current.Loaded:
		current.Unavailable, current.Loaded = true, true
	default:
		current.Failed = true
		current.SignedOut = status == 401
	}
	return current
}

func chatRetentionPreviewPanel(props chatRetentionPreviewProps) ui.Node {
	state := ui.UseState(ChatRetentionPreviewModel{Locale: props.Locale})
	run := ui.UseEvent(func() {
		current := state.Get()
		if current.Loading {
			return
		}
		current.Loading, current.Failed = true, false
		state.Set(current)
		chatRetentionPreviewCall(func(data ChatRetentionPreviewData, status int, err error) {
			state.Set(chatRetentionPreviewAnswered(state.Get(), data, status, err))
		})
	})
	model := state.Get()
	model.Run = run
	return ChatRetentionPreviewSettings(model)
}
