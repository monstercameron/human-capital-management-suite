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
	// WritingStylesNote is why the writing styles are not offered when
	// WritingStyles is false: "not_qualified" (no model has passed the quality
	// check), "workspace_off" (an administrator turned them off) or "unavailable".
	WritingStylesNote string `json:"writing_styles_note,omitempty"`
	// Translation is true when a translation engine is composed, so the
	// administrator's translation settings are offered (CHATLANG-006).
	Translation bool `json:"translation"`
	// Translating is true when the caller's workspace has translation on (an
	// engine is composed and an administrator turned it on): readers are offered
	// the translation setting and a writer is told who reads in which language
	// (CHATLANG-002).
	Translating bool `json:"translating"`
	// Listen is true when the server composed text to speech, so a message offers
	// "Listen" (CHATVOICE-006).
	Listen bool `json:"listen"`
	// ListenBarred names, comma separated, the conversations where Listen is
	// absent because the channel never uses an outside service. It is a string so
	// that the features stay comparable.
	ListenBarred string `json:"listen_barred,omitempty"`
	// Voice is true when the server composed voice messages, so the personal and
	// channel switches are offered (CHATVOICE-005).
	Voice bool `json:"voice"`
}

func integrate2ReadingSettings(m Model) ui.Node {
	if m.ChatFeatures != nil && !m.ChatFeatures.Renderings {
		return nil
	}
	return ui.CreateElement(renderingPersonalPanel, renderingPersonalProps{Locale: m.Locale, Conversation: m.SelectedID})
}
