package productui

import "github.com/monstercameron/human-capital-management-suite/internal/experience/localize"

// agentTaskFilterName is the accessible name of a task filter tab: the category
// and how many tasks it holds ("Completed, 3 tasks"), so the number that sighted
// people see is also heard.
func agentTaskFilterName(locale LocaleContext, category string, count int) string {
	return locale.Plural("agents.filter."+category+"_named", int64(count))
}

func withIssue3982Messages(catalog map[string]map[string]localize.Message) map[string]map[string]localize.Message {
	plural := func(one, other string) localize.Message {
		return localize.Message{Plural: map[string]string{"one": one, "other": other}}
	}
	arabic := func(label string) localize.Message {
		return localize.Message{Plural: map[string]string{
			"zero": label + "، لا مهام", "one": label + "، مهمة واحدة", "two": label + "، مهمتان", "few": label + "، {count} مهام",
			"many": label + "، {count} مهمة", "other": label + "، {count} مهمة",
		}}
	}
	for locale, messages := range map[string]map[string]localize.Message{
		"en-US": {
			"agents.filter.active_named":    plural("Active, {count} task", "Active, {count} tasks"),
			"agents.filter.completed_named": plural("Completed, {count} task", "Completed, {count} tasks"),
			"agents.filter.failed_named":    plural("Failed, {count} task", "Failed, {count} tasks"),
		},
		"de-DE": {
			"agents.filter.active_named":    plural("Aktiv, {count} Aufgabe", "Aktiv, {count} Aufgaben"),
			"agents.filter.completed_named": plural("Abgeschlossen, {count} Aufgabe", "Abgeschlossen, {count} Aufgaben"),
			"agents.filter.failed_named":    plural("Fehlgeschlagen, {count} Aufgabe", "Fehlgeschlagen, {count} Aufgaben"),
		},
		"ar": {
			"agents.filter.active_named":    arabic("نشطة"),
			"agents.filter.completed_named": arabic("مكتملة"),
			"agents.filter.failed_named":    arabic("فاشلة"),
		},
	} {
		for key, message := range messages {
			catalog[locale][key] = message
		}
	}
	return catalog
}
