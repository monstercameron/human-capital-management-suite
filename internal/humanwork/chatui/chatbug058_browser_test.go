package chatui_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATBUG_058_Browser: the whole page in each language holds the
// reading-language row in Conversation details, and nowhere the "Apply to this
// conversation" box.
func TestTodo_CHATBUG_058_Browser(t *testing.T) {
	type words struct{ row, apply string }
	for locale, w := range map[string]words{
		"en-US": {"Reading in this conversation", "Apply to this conversation"},
		"de-DE": {"Lesen in diesem Gespräch", "Auf dieses Gespräch anwenden"},
		"ar":    {"القراءة في هذه المحادثة", "تطبيق على هذه المحادثة"},
	} {
		page := lane3Page(t, locale, func(m *chatui.Model) { m.ShowDetails = true })
		lane3NoLeaks(t, locale+" reading", page)
		if !strings.Contains(page, w.row) || !strings.Contains(page, `id="chatbug058-reading"`) {
			t.Errorf("%s: Conversation details has no reading row", locale)
		}
		if strings.Contains(page, w.apply) || strings.Contains(page, `name="conversation"`) {
			t.Errorf("%s: the page still offers %q", locale, w.apply)
		}
	}
}
