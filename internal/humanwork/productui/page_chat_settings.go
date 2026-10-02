package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func chatSettingsPage(view View) ui.Node {
	if view.Roles != nil && !PageVisible(PageChatSettings, view.Roles) {
		return unavailablePanel(view.Locale.Text("shell.page_unavailable"), view.Locale.Text("shell.page_recovery"))
	}
	if strings.TrimSpace(view.ChatRetentionError) != "" && !view.ChatRetentionConfigured {
		return capabilityUnavailablePanel(view.Locale)
	}
	editable := len(view.EffectivePermissions) == 0 || view.Can(PageAdmin, "update")
	return ui.CreateElement(ChatSettingsPage, ChatSettingsPageProps{
		Settings: ui.CreateElement(ChatRetentionSettings, ChatRetentionSettingsProps{
			I18nProps: I18nProps{Locale: view.Locale}, Configured: view.ChatRetentionConfigured, Policy: view.ChatRetentionPolicy,
			Loading: view.ChatRetentionLoading, Editable: editable,
			Error: view.ChatRetentionError, Notice: view.ChatRetentionNotice, OnSave: view.SaveChatRetentionPolicy,
		}),
		// The workspace's translation settings and glossary (CHATLANG-007).
		Translation: chatui.TranslationAdminSettings(view.Locale.Resolved, func(key string) string { return view.Locale.Text(key) }),
		// The workspace's location sharing settings (CHATMAP-006).
		Location: chatui.ChatmapWorkspaceSettingsPanel(view.Locale.Resolved),
		// The workspace's voice message switch and the engine behind it (CHATVOICE-005).
		Voice: chatui.VoiceWorkspaceSettingsPanel(view.Locale.Resolved),
	})
}
