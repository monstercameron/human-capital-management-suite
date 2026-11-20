package chatui

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

// issue3898NameLimit keeps a very long file name from making the accessible
// name a paragraph.
const issue3898NameLimit = 80

// issue3898Label is the accessible name of the open control of a result that is
// not a message: "Open result: File · team-photo.png · in #people-ops". The
// control used to say only where the result was, so moving between results
// never said which file each one opens.
func issue3898Label(locale string, row chatsearch.Row, wherePlain string) string {
	name := strings.Join(strings.Fields(chatDisplayText(row.Text)), " ")
	if runes := []rune(name); len(runes) > issue3898NameLimit {
		name = string(runes[:issue3898NameLimit]) + "…"
	}
	parts := []string{chatsearchKind(locale, row.Kind)}
	if name != "" {
		parts = append(parts, name)
	}
	if wherePlain != "" && wherePlain != chatsearchKind(locale, row.Kind) {
		parts = append(parts, wherePlain)
	}
	return chatsearchText(locale, "open") + ": " + strings.Join(parts, " · ")
}
