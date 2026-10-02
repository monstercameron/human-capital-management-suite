package chatui

import "strings"

// chatux001Text is the copy of the conversation header's action buttons and of
// the search box's name (CHATUX-001 and CHATUX-010). The words come from the
// tables below, in en-US, de-DE and ar, and from nowhere else: the product
// catalogue answers a key it does not hold with a ⟦key⟧ marker, which must
// never reach a page, so Model.Text is not asked at all.
func chatux001Text(m Model, key string) string {
	table := chatux001En
	switch {
	case strings.HasPrefix(strings.ToLower(m.Locale), "de"):
		table = chatux001De
	case strings.HasPrefix(strings.ToLower(m.Locale), "ar"):
		table = chatux001Ar
	}
	return chatbug039Text(key, table[key], chatux001En[key])
}

var chatux001En = map[string]string{
	"chat.ux001.search":        "Search Chat",
	"chat.ux001.search_hint":   "Search Chat ({shortcut})",
	"chat.ux001.pinned":        "Pinned messages",
	"chat.ux001.pinned_count":  "Pinned messages ({n})",
	"chat.ux001.members":       "Members",
	"chat.ux001.members_count": "Members ({n})",
}

var chatux001De = map[string]string{
	"chat.ux001.search":        "Chat durchsuchen",
	"chat.ux001.search_hint":   "Chat durchsuchen ({shortcut})",
	"chat.ux001.pinned":        "Angeheftete Nachrichten",
	"chat.ux001.pinned_count":  "Angeheftete Nachrichten ({n})",
	"chat.ux001.members":       "Mitglieder",
	"chat.ux001.members_count": "Mitglieder ({n})",
}

var chatux001Ar = map[string]string{
	"chat.ux001.search":        "البحث في الدردشة",
	"chat.ux001.search_hint":   "البحث في الدردشة ({shortcut})",
	"chat.ux001.pinned":        "الرسائل المثبتة",
	"chat.ux001.pinned_count":  "الرسائل المثبتة ({n})",
	"chat.ux001.members":       "الأعضاء",
	"chat.ux001.members_count": "الأعضاء ({n})",
}

func chatux001Textf(m Model, key string, vars map[string]string) string {
	out := chatux001Text(m, key)
	for name, value := range vars {
		out = strings.ReplaceAll(out, "{"+name+"}", value)
	}
	return out
}
