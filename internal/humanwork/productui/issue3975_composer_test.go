package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// #3975: the composer carries the words the browser announces when it quotes a
// request for a follow-up or restores an unsent question, in every language.
func TestIssue3975_ComposerCarriesFollowUpAndRestoreAnnouncements(t *testing.T) {
	seen := map[string]bool{}
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(localeID)
		markup, err := ui.RenderToString(tagAgentComposer(locale, AgentSnapshot{StartAvailable: true}))
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"follow_up", "draft_restored"} {
			text := issue3975Text(locale, key)
			if strings.TrimSpace(text) == "" {
				t.Fatalf("%s %s: empty copy", localeID, key)
			}
			if seen[localeID+text] {
				t.Fatalf("%s: %s repeats another line", localeID, key)
			}
			seen[localeID+text] = true
		}
		for attr, key := range map[string]string{"data-msg-follow-up": "follow_up", "data-msg-draft-restored": "draft_restored"} {
			if !strings.Contains(markup, attr+`="`+issue3975Text(locale, key)+`"`) {
				t.Fatalf("%s: composer status lacks %s: %s", localeID, attr, markup)
			}
		}
		if !strings.Contains(markup, `id="agents-composer-status"`) || !strings.Contains(markup, `aria-live="polite"`) {
			t.Fatalf("%s: the announcements need the polite status region", localeID)
		}
	}
	if issue3975Text(ResolveProductLocale("en-US"), "follow_up") == issue3975Text(ResolveProductLocale("de-DE"), "follow_up") {
		t.Fatal("German follow-up copy is the English text")
	}
}
