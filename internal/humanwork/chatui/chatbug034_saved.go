package chatui

import "strings"

// SavedActionFailedText says that one change to the Saved list did not happen.
// The list had loaded; only the press that was refused is reported, and it names
// what the person was doing so they know what to try again.
func SavedActionFailedText(locale, action string) string {
	kind := "save"
	switch action {
	case "remove":
		kind = "remove"
	case "done", "reopen":
		kind = "state"
	case "note":
		kind = "note"
	case "due", "clear-due":
		kind = "due"
	}
	table := map[string]map[string]string{
		"en": {
			"save":   "We could not save this message. Try again.",
			"remove": "We could not remove this message from Saved. Try again.",
			"state":  "We could not update this message. Try again.",
			"note":   "We could not save your note. Try again.",
			"due":    "We could not save your reminder. Try again.",
		},
		"de": {
			"save":   "Die Nachricht konnte nicht gespeichert werden. Versuchen Sie es erneut.",
			"remove": "Die Nachricht konnte nicht aus Gespeichert entfernt werden. Versuchen Sie es erneut.",
			"state":  "Die Nachricht konnte nicht aktualisiert werden. Versuchen Sie es erneut.",
			"note":   "Ihre Notiz konnte nicht gespeichert werden. Versuchen Sie es erneut.",
			"due":    "Ihre Erinnerung konnte nicht gespeichert werden. Versuchen Sie es erneut.",
		},
		"ar": {
			"save":   "تعذّر حفظ هذه الرسالة. حاول مرة أخرى.",
			"remove": "تعذّرت إزالة هذه الرسالة من المحفوظات. حاول مرة أخرى.",
			"state":  "تعذّر تحديث هذه الرسالة. حاول مرة أخرى.",
			"note":   "تعذّر حفظ ملاحظتك. حاول مرة أخرى.",
			"due":    "تعذّر حفظ تذكيرك. حاول مرة أخرى.",
		},
	}
	language := "en"
	switch {
	case strings.HasPrefix(locale, "de"):
		language = "de"
	case strings.HasPrefix(locale, "ar"):
		language = "ar"
	}
	return chatbug039Text(kind, table[language][kind], table["en"][kind])
}
