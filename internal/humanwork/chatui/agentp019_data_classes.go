package chatui

import (
	"strings"
	"unicode"
)

// personaDataClassCopy names the registry's data classes for a member, in
// en-US, de-DE and ar. The words for compensation match Agent setup.
var personaDataClassCopy = map[string][3]string{
	"PUBLIC":       {"Public information", "Öffentliche Informationen", "معلومات عامة"},
	"INTERNAL":     {"Internal information", "Interne Informationen", "معلومات داخلية"},
	"POLICY":       {"Reads policy documents", "Liest Richtliniendokumente", "يقرأ وثائق السياسات"},
	"WORKFORCE":    {"Workforce records", "Personaldaten", "سجلات القوى العاملة"},
	"SCHEDULE":     {"Schedules", "Dienstpläne", "جداول العمل"},
	"ONBOARDING":   {"Onboarding records", "Onboarding-Unterlagen", "سجلات التهيئة"},
	"COMPENSATION": {"Compensation information", "Vergütungsinformationen", "معلومات التعويضات"},
	"OTHER":        {"Other information", "Sonstige Informationen", "معلومات أخرى"},
}

// personaDataClassLabel is the member-facing name of one data class. A class
// this page has no words for is still shown, as its identifier turned into
// plain words ("PAYROLL_LEDGER" reads "Payroll ledger"): leaving it out would
// tell the member the agent reaches less than it does. A value the server
// already wrote as words is passed through.
func personaDataClassLabel(locale, class string) string {
	class = strings.TrimSpace(class)
	if class == "" {
		return ""
	}
	if class == "POLICY_DOCUMENT" {
		return chatPolishPolicyScope(locale)
	}
	if labels, ok := personaDataClassCopy[strings.ToUpper(class)]; ok {
		return labels[chatbug039LocaleIndex(locale)]
	}
	if strings.Contains(class, " ") && !strings.Contains(class, "_") && !strings.Contains(class, "hcmnext.") {
		return class
	}
	// Identifiers such as "hcmnext.data.payroll_ledger" keep their last part.
	if dot := strings.LastIndexByte(class, '.'); dot >= 0 {
		class = class[dot+1:]
	}
	words := strings.Fields(strings.ToLower(strings.NewReplacer("_", " ", "-", " ").Replace(class)))
	if len(words) == 0 {
		return ""
	}
	first := []rune(words[0])
	first[0] = unicode.ToUpper(first[0])
	words[0] = string(first)
	return strings.Join(words, " ")
}
