package chatui

import "strings"

// CHATUX-031 (lane W3): the line under a linked document's title names where
// the document comes from, its collection or, when the service sends none, what
// kind of document it is. It used to name the owner, and in a thread the owner
// read as the person who had asked. The owner stays on the card as its tooltip.
const keyChatux031DocSource = "chat.doc_source_default"

var chatux031DocSourceCopy = map[string]string{
	"en-US": "Policy document",
	"de-DE": "Richtliniendokument",
	"ar":    "مستند سياسة",
}

// chatux031DocSourceLine is the card's byline: the source, then the date the
// document was last updated.
func chatux031DocSourceLine(m Model, preview DocPreview) string {
	source := strings.TrimSpace(preview.Collection)
	if source == "" {
		source = chatbug039Text(keyChatux031DocSource, chatux031DocSourceCopy[chatEmojiLocale(m.Locale)], chatux031DocSourceCopy["en-US"])
	}
	if preview.UpdatedAt != "" {
		return strings.TrimSpace(source + " · " + preview.UpdatedAt)
	}
	return source
}
