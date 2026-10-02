package chatui

import "strings"

var chat5Copy = map[string]map[string]string{
	"en-US": {
		"chat.agents.here":            "Agents here",
		"chat.agents.count":           "{n} agents",
		"chat.agents.ask":             "Ask",
		"chat.agents.reads_channel":   "Reads documents placed in this channel",
		"chat.agents.reads_workspace": "Reads workspace documents",
		"chat.agents.manage":          "Manage in Agent setup",
		"chat.emoji.search":           "Search emoji",
		"chat.emoji.recent":           "Recent",
		"chat.emoji.faces":            "Faces",
		"chat.emoji.symbols":          "Symbols",
		"chat.markdown":               "Markdown",
		"chat.todo.empty_next":        "No tasks yet",
		"chat.todo.add_next":          "Add a task",
	},
	"de-DE": {
		"chat.agents.here":            "Agenten hier",
		"chat.agents.count":           "{n} Agenten",
		"chat.agents.ask":             "Fragen",
		"chat.agents.reads_channel":   "Liest Dokumente in diesem Kanal",
		"chat.agents.reads_workspace": "Liest Arbeitsbereichsdokumente",
		"chat.agents.manage":          "In der Agenteneinrichtung verwalten",
		"chat.emoji.search":           "Emoji suchen",
		"chat.emoji.recent":           "Zuletzt verwendet",
		"chat.emoji.faces":            "Gesichter",
		"chat.emoji.symbols":          "Symbole",
		"chat.markdown":               "Markdown",
		"chat.todo.empty_next":        "Noch keine Aufgaben",
		"chat.todo.add_next":          "Aufgabe hinzufügen",
	},
	"ar": {
		"chat.agents.here":            "الوكلاء هنا",
		"chat.agents.count":           "{n} وكلاء",
		"chat.agents.ask":             "اسأل",
		"chat.agents.reads_channel":   "يقرأ المستندات الموضوعة في هذه القناة",
		"chat.agents.reads_workspace": "يقرأ مستندات مساحة العمل",
		"chat.agents.manage":          "إدارة في إعداد الوكيل",
		"chat.emoji.search":           "البحث عن رمز تعبيري",
		"chat.emoji.recent":           "الأخيرة",
		"chat.emoji.faces":            "الوجوه",
		"chat.emoji.symbols":          "الرموز",
		"chat.markdown":               "Markdown",
		"chat.todo.empty_next":        "لا توجد مهام بعد",
		"chat.todo.add_next":          "إضافة مهمة",
	},
}

func chat5Text(m Model, key string) string {
	return chatbug039Text(key, chat5Copy[chatEmojiLocale(m.Locale)][key], chat5Copy["en-US"][key])
}
func chat5Format(m Model, key string, args map[string]string) string {
	value := chat5Text(m, key)
	for name, arg := range args {
		value = strings.ReplaceAll(value, "{"+name+"}", arg)
	}
	return value
}
