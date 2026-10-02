package chatui

// CHATUX-020 owns the strings of the sidebar's row menu and draft mark, in
// en-US, de-DE and ar.
const (
	keyChatux020MarkRead      = "chat.ux020.mark_read"
	keyChatux020MarkUnread    = "chat.ux020.mark_unread"
	keyChatux020Notifications = "chat.ux020.notifications"
	keyChatux020Leave         = "chat.ux020.leave"
	keyChatux020LeaveConfirm  = "chat.ux020.leave_confirm"
	keyChatux020Draft         = "chat.ux020.draft"
	keyChatux020Reorder       = "chat.ux020.reorder"
)

var chatux020Copy = map[string]map[string]string{
	"en-US": {
		keyChatux020MarkRead:      "Mark as read",
		keyChatux020MarkUnread:    "Mark as unread",
		keyChatux020Notifications: "Notify me about",
		keyChatux020Leave:         "Leave channel",
		keyChatux020LeaveConfirm:  "Leave {name}? Press again to confirm",
		keyChatux020Draft:         "Unsent draft",
		keyChatux020Reorder:       "Reorder",
	},
	"de-DE": {
		keyChatux020MarkRead:      "Als gelesen markieren",
		keyChatux020MarkUnread:    "Als ungelesen markieren",
		keyChatux020Notifications: "Benachrichtigen bei",
		keyChatux020Leave:         "Kanal verlassen",
		keyChatux020LeaveConfirm:  "{name} verlassen? Zum Bestätigen erneut drücken",
		keyChatux020Draft:         "Nicht gesendeter Entwurf",
		keyChatux020Reorder:       "Neu anordnen",
	},
	"ar": {
		keyChatux020MarkRead:      "تحديد كمقروءة",
		keyChatux020MarkUnread:    "تحديد كغير مقروءة",
		keyChatux020Notifications: "أبلغني عن",
		keyChatux020Leave:         "مغادرة القناة",
		keyChatux020LeaveConfirm:  "مغادرة {name}؟ اضغط مرة أخرى للتأكيد",
		keyChatux020Draft:         "مسودة غير مرسلة",
		keyChatux020Reorder:       "إعادة الترتيب",
	},
}
