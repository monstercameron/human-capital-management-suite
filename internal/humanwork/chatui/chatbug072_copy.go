package chatui

import "strings"

// CHATBUG-072 owns the strings below. Each key has an en-US, de-DE and ar
// entry, so neither the create dialog nor the search bar can print a copy key.
const (
	keyChatbug072NameRule   = "chat.bug072.name_rule"
	keyChatbug072ScopeOffer = "chat.bug072.scope_offer"
)

var chatbug072Copy = map[string]map[string]string{
	"en-US": {
		keyChatbug072NameRule:   "Use letters, numbers, dashes and underscores, up to 80 characters.",
		keyChatbug072ScopeOffer: "Search only in {channel}",
	},
	"de-DE": {
		keyChatbug072NameRule:   "Buchstaben, Ziffern, Bindestriche und Unterstriche, höchstens 80 Zeichen.",
		keyChatbug072ScopeOffer: "Nur in {channel} suchen",
	},
	"ar": {
		keyChatbug072NameRule:   "استخدم الأحرف والأرقام والشرطات والشرطات السفلية، بحد أقصى 80 حرفاً.",
		keyChatbug072ScopeOffer: "البحث في {channel} فقط",
	},
}

// laneText answers one key of a feature copy table for the model's locale,
// falling back to English, and never to the key itself.
func laneText(m Model, table map[string]map[string]string, key string) string {
	return chatbug039Text(key, table[chatEmojiLocale(m.Locale)][key], table["en-US"][key])
}

// laneTextf is laneText with {name} placeholders filled from vars.
func laneTextf(m Model, table map[string]map[string]string, key string, vars map[string]string) string {
	out := laneText(m, table, key)
	for name, value := range vars {
		out = strings.ReplaceAll(out, "{"+name+"}", value)
	}
	return out
}
