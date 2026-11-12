package chatui

import (
	"strings"
	"testing"
)

// TestTodo_ISSUE_3704_PersonalVoiceHintPadded: the personal Voice messages row
// in Chat preferences wraps its help text in the same padded body as Quiet
// hours (.rail-prefs-body, 12 px inline padding). Unwrapped, the help text sat
// flush against the popover edge.
func TestTodo_ISSUE_3704_PersonalVoiceHintPadded(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		got := voiceSettingsRender(t, voiceSettingsProps{Locale: locale, Scope: voiceScopePerson}, voiceSettingsState{Loaded: true})
		hint := strings.Index(got, `id="chatvoice-switch-person-hint"`)
		if hint < 0 {
			t.Fatalf("%s: the personal row has no help text: %s", locale, got)
		}
		wrap := strings.LastIndex(got[:hint], `class="rail-prefs-body"`)
		if wrap < 0 || strings.Contains(got[wrap:hint], `class="chat-prefs-head`) {
			t.Fatalf("%s: the personal row's help text is not inside the padded body (.rail-prefs-body): %s", locale, got)
		}
	}
}
