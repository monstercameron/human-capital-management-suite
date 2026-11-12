package chatui

import (
	"strings"
	"testing"
)

// TestIssue3677_ArabicChannelPlaceholderKeepsHashWithName: in an Arabic page
// the message box names the channel as "#qa-smoke". The "#" is a neutral, so
// without an isolate the right-to-left paragraph draws it after the name
// ("qa-smoke#"); the channel token is isolated as one left-to-right unit.
// English keeps the plain "Message #qa-smoke".
func TestIssue3677_ArabicChannelPlaceholderKeepsHashWithName(t *testing.T) {
	fsi, pdi := string(chatux043FSI), string(chatux043PDI)
	for _, locale := range []string{"ar", "en-US"} {
		m := composerToolsModel(locale, false)
		found := false
		for i := range m.Conversations {
			if m.Conversations[i].Kind == PublicChannel {
				m.Conversations[i].Name = "qa-smoke"
				m.SelectedID = m.Conversations[i].ID
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%s: the fixture has no public channel to select", locale)
		}
		markup := composerToolsMarkup(t, m, localUI{})
		if locale == "en-US" {
			if !strings.Contains(markup, `placeholder="Message #qa-smoke"`) {
				t.Errorf("en-US placeholder is not %q in %s", "Message #qa-smoke", markup)
			}
			continue
		}
		if strings.Contains(markup, "qa-smoke#") {
			t.Errorf("ar placeholder puts the hash after the channel name: %s", markup)
		}
		if !strings.Contains(markup, fsi+"#qa-smoke"+pdi) {
			t.Errorf("ar placeholder does not isolate the channel token %q in %s", fsi+"#qa-smoke"+pdi, markup)
		}
		// The key names in the Arabic composer hint stay isolated, each with
		// its phrase (CHATUX-043).
		for _, key := range []string{`<bdi dir="ltr">Enter</bdi>`, `<bdi dir="ltr">Shift+Enter</bdi>`} {
			if !strings.Contains(markup, key) {
				t.Errorf("ar composer hint lacks %s in %s", key, markup)
			}
		}
	}
}
