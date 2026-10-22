package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// RenderModAdminPanelHTML keeps GWC's node and HTML serialization boundary in
// the chat UI package. Application code should pass only the resulting markup.
func RenderModAdminPanelHTML(props ModAdminProps) (string, error) {
	return ui.RenderToString(ModAdminPanel(props))
}

// RenderModerationPageHTML serializes the chat-owned moderation page model.
func RenderModerationPageHTML(model ModerationPageModel) (string, error) {
	return ui.RenderToString(ModerationPage(model))
}

// RenderModerationDialogHTML serializes the chat-owned moderation dialog model.
func RenderModerationDialogHTML(model ModerationDialogModel) (string, error) {
	return ui.RenderToString(ModerationDialog(model))
}
