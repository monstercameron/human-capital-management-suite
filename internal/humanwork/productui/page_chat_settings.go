package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

func chatSettingsPage(view View) ui.Node {
	if view.Roles != nil && !PageVisible(PageChatSettings, view.Roles) {
		return unavailablePanel(view.Locale.Text("shell.page_unavailable"), view.Locale.Text("shell.page_recovery"))
	}
	editable := len(view.EffectivePermissions) == 0 || view.Can(PageAdmin, "update")
	return ui.CreateElement(ChatSettingsPage, ChatSettingsPageProps{
		Settings: ui.CreateElement(ChatRetentionSettings, ChatRetentionSettingsProps{
			I18nProps: I18nProps{Locale: view.Locale}, Configured: view.ChatRetentionConfigured, Policy: view.ChatRetentionPolicy,
			Loading: view.ChatRetentionLoading, Editable: editable,
			Error: view.ChatRetentionError, Notice: view.ChatRetentionNotice, OnSave: view.SaveChatRetentionPolicy,
		}),
	})
}
