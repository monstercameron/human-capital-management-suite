package chatui

// ISSUE-3714 owns the strings below. Each key has an en-US, de-DE and ar entry,
// so the create dialog never prints a copy key.
const keyChatbug3714NameRequired = "chat.bug3714.name_required"

var chatbug3714Copy = map[string]map[string]string{
	"en-US": {keyChatbug3714NameRequired: "Name is required."},
	"de-DE": {keyChatbug3714NameRequired: "Name ist erforderlich."},
	"ar":    {keyChatbug3714NameRequired: "الاسم مطلوب."},
}

// chatbug3714NameNote is the reason printed under a channel's name box when the
// channel was submitted with no name. It is empty for every other kind and once
// the person has typed again (the submit clears the flag on the next input).
func chatbug3714NameNote(m Model, local localUI, kind ConversationKind) string {
	if !channelNamed(kind) || !local.nameMissing {
		return ""
	}
	return laneText(m, chatbug3714Copy, keyChatbug3714NameRequired)
}
