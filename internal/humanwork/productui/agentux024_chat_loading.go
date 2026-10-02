package productui

import "github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"

// chatLoadingModel is the model Chat's loading, content-loading and failure
// proxies are drawn from (AGENTUX-024). It carries the viewer's locale, writing
// direction and catalog exactly as BuildChatPage does for a loaded page, so the
// first paint is in the viewer's language and mirrored for right-to-left
// readers rather than English until the first conversation arrives. A proxy
// built without a locale (a component preview) keeps the English default.
func chatLoadingModel(locale LocaleContext) chatui.Model {
	model := chatui.Model{State: chatui.StateLoading}
	if locale.Resolved == "" {
		return model
	}
	locale = locale.normalized()
	model.Locale = locale.Resolved
	model.Direction = string(locale.Direction)
	model.Text = func(key string) string { return locale.Text(key) }
	return model
}
