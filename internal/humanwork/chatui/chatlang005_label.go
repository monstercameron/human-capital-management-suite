package chatui

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
)

// ChatlangSourceLabel names a document or answer together with the language it
// is in, for a reader who reads another: "Paid time off policy, in English"
// (CHATLANG-005). A title already in the reader's language, or a language the
// product does not name, is left as it is.
func ChatlangSourceLabel(locale, title, language string) string {
	tag := chatrender.Language(language)
	if tag == "" || tag == "und" || tag == "mul" || !chatrender.Supported(tag) || tag == chatrender.Language(locale) {
		return title
	}
	name := RenderingText(locale, tag)
	switch {
	case strings.HasPrefix(strings.ToLower(locale), "de"):
		return title + ", auf " + name
	case strings.HasPrefix(strings.ToLower(locale), "ar"):
		return title + "، باللغة " + name
	}
	return title + ", in " + name
}
