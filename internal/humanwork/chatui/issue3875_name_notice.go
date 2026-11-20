package chatui

import "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"

// Issue 3875: when the name box removes a disallowed character and the name
// that is left is valid, the hint says what happened without marking the box
// invalid. A name that is still unusable, or a service refusal, stays an error.
const keyChatissue3875Removed = "chat.issue3875.removed"

var chatissue3875Copy = map[string]map[string]string{
	"en-US": {keyChatissue3875Removed: "A character that names cannot use was removed. Names use letters, numbers, dashes and underscores, up to 80 characters."},
	"de-DE": {keyChatissue3875Removed: "Ein Zeichen, das in Namen nicht erlaubt ist, wurde entfernt. Namen bestehen aus Buchstaben, Ziffern, Bindestrichen und Unterstrichen, höchstens 80 Zeichen."},
	"ar":    {keyChatissue3875Removed: "تمت إزالة حرف لا يمكن استخدامه في الأسماء. تتكوّن الأسماء من أحرف وأرقام وشرطات وشرطات سفلية، بحد أقصى 80 حرفاً."},
}

// chatissue3875Removed reports whether the only thing wrong is a character the
// box already removed: the typed name is valid as it stands.
func chatissue3875Removed(m Model, local localUI, kind ConversationKind) bool {
	if !channelNamed(kind) || !local.nameRefused || !chat.ValidChannelName(local.nameTyped) {
		return false
	}
	return !(m.NewNameTaken != "" && m.NewNameTaken == local.nameTyped) &&
		!(m.NewNameRefused != "" && m.NewNameRefused == local.nameTyped)
}
