package chatui

// Issue 3820: the question editor's retention input is labelled in plain words.
// The "days" key is a count format ("%d days") used for durations, so the label
// has its own key.
func init() {
	for locale, label := range map[string]string{
		"en-US": "Keep answers for (days)",
		"de-DE": "Antworten aufbewahren (Tage)",
		"ar":    "مدة الاحتفاظ بالإجابات (بالأيام)",
	} {
		chatgateExtraCopy[locale]["retentionLabel"] = label
	}
}
