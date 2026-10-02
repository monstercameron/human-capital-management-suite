package chatui

import "strings"

// CHATUX-002 and CHATUX-007 own the strings below. Each key has an en-US,
// de-DE and ar entry; a deployment's Model.Text wins when it supplies one, and
// the table answers otherwise, so a control never shows a copy key.
const (
	keyChatux002Prefs       = "chat.ux002.prefs"
	keyChatux002QuietOnTip  = "chat.ux002.quiet_on_tip"
	keyChatux002Change      = "chat.ux002.change"
	keyChatux002ChannelMenu = "chat.ux002.channel_menu"
	keyChatux002AddName     = "chat.ux002.add_name"
	keyChatux002AddNote     = "chat.ux002.add_note"
	keyChatux002BrowseNote  = "chat.ux002.browse_note"
	keyChatux002SectionNote = "chat.ux002.section_note"
	keyChatux007Jump        = "chat.ux007.jump_unread"
)

var chatux002Copy = map[string]map[string]string{
	"en-US": {
		keyChatux002Prefs:       "Chat preferences",
		keyChatux002QuietOnTip:  "Quiet hours are on: {from}–{until}",
		keyChatux002Change:      "Change",
		keyChatux002ChannelMenu: "Add or find channels",
		keyChatux002AddName:     "Create a channel",
		keyChatux002AddNote:     "Start a team or topic channel",
		keyChatux002BrowseNote:  "Find and join channels",
		keyChatux002SectionNote: "Group your conversations",
		keyChatux007Jump:        "Jump to unread",
	},
	"de-DE": {
		keyChatux002Prefs:       "Chat-Einstellungen",
		keyChatux002QuietOnTip:  "Ruhezeiten sind aktiv: {from}–{until}",
		keyChatux002Change:      "Ändern",
		keyChatux002ChannelMenu: "Kanäle hinzufügen oder finden",
		keyChatux002AddName:     "Kanal erstellen",
		keyChatux002AddNote:     "Kanal für Team oder Thema",
		keyChatux002BrowseNote:  "Kanäle finden und beitreten",
		keyChatux002SectionNote: "Unterhaltungen gruppieren",
		keyChatux007Jump:        "Zu Ungelesenem springen",
	},
	"ar": {
		keyChatux002Prefs:       "تفضيلات الدردشة",
		keyChatux002QuietOnTip:  "ساعات الهدوء مفعّلة: {from}–{until}",
		keyChatux002Change:      "تغيير",
		keyChatux002ChannelMenu: "إضافة قنوات أو العثور عليها",
		keyChatux002AddName:     "إنشاء قناة",
		keyChatux002AddNote:     "قناة لفريق أو موضوع",
		keyChatux002BrowseNote:  "ابحث عن القنوات وانضم إليها",
		keyChatux002SectionNote: "تجميع محادثاتك",
		keyChatux007Jump:        "الانتقال إلى غير المقروء",
	},
}

// chatux002Text answers one of the strings above for the model's locale.
func chatux002Text(m Model, key string) string {
	return chatbug039Text(key, chatux002Copy[chatEmojiLocale(m.Locale)][key], chatux002Copy["en-US"][key])
}

func chatux002Textf(m Model, key string, vars map[string]string) string {
	out := chatux002Text(m, key)
	for name, value := range vars {
		out = strings.ReplaceAll(out, "{"+name+"}", value)
	}
	return out
}
