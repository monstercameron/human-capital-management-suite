package chatui

import "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"

// issue3682ReadingLanguage is the reading language a person is shown: the one
// they chose, or, when nothing usable is stored (empty or "und"), their
// interface language, the same default the service applies. Without it no
// option is marked selected and the browser shows the last one.
func issue3682ReadingLanguage(locale, stored string) string {
	l := chatrender.Language(stored)
	if l == "" || l == "und" || !chatrender.Supported(l) {
		return chatrender.Language(chatrender.DefaultPreference(locale).ReadingLanguage)
	}
	return l
}
