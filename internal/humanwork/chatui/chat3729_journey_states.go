package chatui

// journeyStateWords is the copy for the journey card's failure states, in
// English, German and Arabic. A viewer without a grant, a journey that no
// longer exists and a temporary failure each get their own sentence (#3729).
var journeyStateWords = map[string][3]string{
	"restricted":  {"You do not have access to this workflow.", "Sie haben keinen Zugriff auf diesen Workflow.", "ليست لديك صلاحية الوصول إلى هذا المسار."},
	"missing":     {"This workflow was not found. It may have been removed.", "Dieser Workflow wurde nicht gefunden. Er wurde möglicherweise entfernt.", "لم يُعثر على هذا المسار. ربما أُزيل."},
	"unavailable": {"Could not load this workflow. Try again in a moment.", "Dieser Workflow konnte nicht geladen werden. Versuchen Sie es gleich erneut.", "تعذّر تحميل هذا المسار. حاول مرة أخرى بعد قليل."},
}

// journeyLockedState maps a card state onto one of the failure states. Any
// other state falls back to no access, so a locked card never shows a
// misleading temporary-failure sentence.
func journeyLockedState(state string) string {
	if _, ok := journeyStateWords[state]; ok {
		return state
	}
	return "restricted"
}

func journeyStateText(m Model, state string) string {
	words := journeyStateWords[state]
	return chatbug039Text(state, words[chatbug039LocaleIndex(m.Locale)], words[0])
}
