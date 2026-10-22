package chatui

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// TestTodo_CHATBUG_100: Chat preferences never strands a block on a spinner,
// its languages line agrees with the Reading setting beside it, and its
// controls sit at the row end at every width.
func TestTodo_CHATBUG_100(t *testing.T) {
	t.Run("a failed voice read ends in the retry state", func(t *testing.T) {
		next, restore := voiceSettingsAnswered(voiceSettingsState{}, VoiceSettingsData{}, 0, errors.New("down"), false)
		if !next.LoadFailed || next.Loading || restore {
			t.Fatalf("fresh failed read = %+v restore=%v", next, restore)
		}
		if got := voiceSettingsRender(t, voiceSettingsProps{Locale: "en-US", Scope: voiceScopePerson}, next); !strings.Contains(got, "Voice settings could not load. Try again.") || !strings.Contains(got, ">Try again<") {
			t.Fatalf("no retry state: %s", got)
		}
		loaded := voiceSettingsState{Loaded: true, Data: VoiceSettingsData{Person: true}}
		next, restore = voiceSettingsAnswered(loaded, VoiceSettingsData{}, 0, errors.New("down"), false)
		if !next.Fails || !restore {
			t.Fatalf("failed save = %+v restore=%v", next, restore)
		}
		// The client-side bound exists past the request timeout, so a read
		// that never answers still ends in the retry state above.
		if voiceSettingsReadBound != 10*time.Second {
			t.Fatalf("voice read bound = %v", voiceSettingsReadBound)
		}
	})

	t.Run("a voice feature that is not set up says so in one line", func(t *testing.T) {
		got := voiceSettingsRender(t, voiceSettingsProps{Locale: "en-US", Scope: voiceScopeWorkspace}, voiceSettingsState{Loaded: true, Unavailable: true})
		if !strings.Contains(got, "Voice messages are not set up on this server.") || strings.Contains(got, `role="switch"`) || strings.Contains(got, "aria-busy") {
			t.Fatalf("unset workspace voice: %s", got)
		}
	})

	t.Run("the languages line reflects the reading setting", func(t *testing.T) {
		withSetting := renderNode(t, RenderingLanguageStatusIndicatorWithReading("en-US", nil, false, false, "en"))
		if !strings.Contains(withSetting, "English") || strings.Contains(withSetting, "Set your reading language") {
			t.Fatalf("setting ignored: %s", withSetting)
		}
		without := renderNode(t, RenderingLanguageStatusIndicatorWithReading("en-US", nil, false, false, ""))
		if !strings.Contains(without, "Set your reading language") {
			t.Fatalf("missing prompt: %s", without)
		}
	})

	t.Run("controls sit at the row end at every width", func(t *testing.T) {
		if !strings.Contains(ChatUX031Styles, ".chat-workspace .chatux031-select{flex:0 1 auto;margin-inline-start:auto") {
			t.Error("the reading select lost its row-end margin")
		}
		if !strings.Contains(ChatUX031Styles, "@container(max-width:250px){.chat-workspace .chat-prefs-head .chat-prefs-title{flex:1 0 100%}}") {
			t.Error("the narrow panel no longer wraps the title above an end-aligned select")
		}
		if strings.Contains(ChatUX031Styles, ".chat-prefs-head>.chatux031-select{margin-inline-start:0}") {
			t.Error("the narrow panel still parks the select at the line start")
		}
	})
}
