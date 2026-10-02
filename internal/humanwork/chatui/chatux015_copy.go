package chatui

// CHATUX-015 owns the Create conversation cards' wording, in en-US, de-DE and
// ar: each kind is explained by what it is for, so a person can tell a private
// channel from a group message before choosing.
const (
	keyChatux015PublicNote  = "chat.ux015.kind_public_note"
	keyChatux015PrivateNote = "chat.ux015.kind_private_note"
	keyChatux015GroupTitle  = "chat.ux015.kind_group_title"
	keyChatux015GroupNote   = "chat.ux015.kind_group_note"
)

var chatux015Copy = map[string]map[string]string{
	"en-US": {
		keyChatux015PublicNote:  "A topic or team that anyone in the company can find and join.",
		keyChatux015PrivateNote: "A lasting topic for invited people.",
		keyChatux015GroupTitle:  "Group message",
		keyChatux015GroupNote:   "A quick conversation with a few people.",
	},
	"de-DE": {
		keyChatux015PublicNote:  "Ein Thema oder Team, das alle im Unternehmen finden und dem sie beitreten können.",
		keyChatux015PrivateNote: "Ein dauerhaftes Thema für eingeladene Personen.",
		keyChatux015GroupTitle:  "Gruppennachricht",
		keyChatux015GroupNote:   "Eine kurze Unterhaltung mit wenigen Personen.",
	},
	"ar": {
		keyChatux015PublicNote:  "موضوع أو فريق يمكن لأي شخص في الشركة العثور عليه والانضمام إليه.",
		keyChatux015PrivateNote: "موضوع دائم للأشخاص المدعوين.",
		keyChatux015GroupTitle:  "رسالة جماعية",
		keyChatux015GroupNote:   "محادثة سريعة مع عدد قليل من الأشخاص.",
	},
}

// chatux015Kind is the title and the use of one Create conversation card.
func chatux015Kind(m Model, kind ConversationKind, titleKey, noteKey string) (title, note string) {
	title, note = m.t(titleKey), m.t(noteKey)
	switch kind {
	case PublicChannel:
		note = laneText(m, chatux015Copy, keyChatux015PublicNote)
	case PrivateChannel:
		note = laneText(m, chatux015Copy, keyChatux015PrivateNote)
	case GroupChat:
		title, note = laneText(m, chatux015Copy, keyChatux015GroupTitle), laneText(m, chatux015Copy, keyChatux015GroupNote)
	}
	return title, note
}
