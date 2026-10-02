package chatui

// CHATBUG-068 owns the strings below, each in en-US, de-DE and ar.
const (
	keyChatbug068Unreadable     = "chat.bug068.unreadable"
	keyChatbug068DraftElsewhere = "chat.bug068.draft_elsewhere"
)

var chatbug068Copy = map[string]map[string]string{
	"en-US": {
		keyChatbug068Unreadable:     "This conversation is no longer available to you.",
		keyChatbug068DraftElsewhere: "Draft updated on another device. What you are typing here is kept.",
	},
	"de-DE": {
		keyChatbug068Unreadable:     "Diese Unterhaltung ist für dich nicht mehr verfügbar.",
		keyChatbug068DraftElsewhere: "Entwurf auf einem anderen Gerät geändert. Dein Text hier bleibt erhalten.",
	},
	"ar": {
		keyChatbug068Unreadable:     "لم تعد هذه المحادثة متاحة لك.",
		keyChatbug068DraftElsewhere: "تم تحديث المسودة على جهاز آخر. يبقى ما تكتبه هنا كما هو.",
	},
}

// ConversationUnreadableText is the line the open conversation shows, in place,
// when it stops being one the person may read. The page never moves to another
// conversation to say it.
func ConversationUnreadableText(locale string) string {
	return laneText(Model{Locale: locale}, chatbug068Copy, keyChatbug068Unreadable)
}

// DraftUpdatedElsewhereText is the notice shown when another session of the
// same person saved a different draft for the open conversation: the composer
// keeps what is typed in it and the person is told, rather than the text being
// replaced.
func DraftUpdatedElsewhereText(locale string) string {
	return laneText(Model{Locale: locale}, chatbug068Copy, keyChatbug068DraftElsewhere)
}
