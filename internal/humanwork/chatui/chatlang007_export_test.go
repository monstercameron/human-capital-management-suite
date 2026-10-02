package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// TranslationSettingsEntryForTest is the Translation entry of Conversation
// details, for the external tests that decide who is offered it.
func TranslationSettingsEntryForTest(m Model, c Conversation) ui.Node {
	return translationSettingsEntry(m, c)
}
