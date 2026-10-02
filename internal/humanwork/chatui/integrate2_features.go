package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

type ChatFeatures struct {
	Gates         bool `json:"gates"`
	Renderings    bool `json:"renderings"`
	Status        bool `json:"status"`
	Search        bool `json:"search"`
	Filters       bool `json:"filters"`
	Locations     bool `json:"locations"`
	WritingStyles bool `json:"writing_styles"`
	// Translation is true when a translation engine is composed, so the
	// administrator's translation settings are offered (CHATLANG-006).
	Translation bool `json:"translation"`
	// Translating is true when the caller's workspace has translation on (an
	// engine is composed and an administrator turned it on): readers are offered
	// the translation setting and a writer is told who reads in which language
	// (CHATLANG-002).
	Translating bool `json:"translating"`
}

func integrate2ReadingSettings(m Model) ui.Node {
	if m.ChatFeatures != nil && !m.ChatFeatures.Renderings {
		return nil
	}
	return ui.CreateElement(renderingPersonalPanel, renderingPersonalProps{Locale: m.Locale, Conversation: m.SelectedID})
}
