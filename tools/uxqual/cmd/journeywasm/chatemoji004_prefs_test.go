package main

import (
	"os"
	"strings"
	"testing"
)

// CHATEMOJI-004: the voice playback speed and the emoji choices are kept with the
// reader's other personal Chat preferences on the server (the sidebar layout the
// client already reads at load and writes after a change), not only for the life
// of the page.
func TestTodo_CHATEMOJI_004_VoiceSpeedIsAStoredPreference(t *testing.T) {
	p := newVoicePlayback()
	if err := p.SetSpeed(1.5); err != nil {
		t.Fatal(err)
	}
	p.Collapsed = false
	stored := voicePreferenceEncode(p)
	again := voicePreferenceDecode(stored)
	if again.Speed != 1.5 || again.Collapsed {
		t.Fatalf("speed and collapsed did not round-trip through %q: %+v", stored, again)
	}
	// A value the player does not offer is not believed.
	if got := voicePreferenceDecode(`{"Speed":7,"Collapsed":false}`); got.Speed != 1 || !got.Collapsed {
		t.Fatalf("an unknown speed was believed: %+v", got)
	}
}

func TestTodo_CHATEMOJI_004_ClientWritesAndReadsTheLayoutKeys(t *testing.T) {
	read := func(name string) string {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	recipient := read("chat_recipient_wasm.go")
	for _, want := range []string{
		"EmojiPrefs string `json:\"emojiPrefs,omitempty\"`",
		"VoicePrefs string `json:\"voicePrefs,omitempty\"`",
		"callbacks.SaveEmojiPrefs = func(encoded string)",
		"model.EmojiPrefs = layout.EmojiPrefs",
		"layout.EmojiPrefs, layout.VoicePrefs = model.EmojiPrefs, chatRecipientBrowser.layout.VoicePrefs",
		"chatui.MergeEmojiPrefs(server.EmojiPrefs, emojiWritten)",
	} {
		if !strings.Contains(recipient, want) {
			t.Errorf("chat_recipient_wasm.go no longer carries %q", want)
		}
	}
	voice := read("chatvoice_playback_wasm.go")
	for _, want := range []string{"chatRecipientBrowser.layout.VoicePrefs = raw", "persistChatRecipientSidebar(b.config, chatBrowser.snapshot())", "raw := chatRecipientBrowser.layout.VoicePrefs"} {
		if !strings.Contains(voice, want) {
			t.Errorf("chatvoice_playback_wasm.go no longer carries %q", want)
		}
	}
	for _, forbidden := range []string{"localStorage", "sessionStorage", "indexedDB"} {
		if strings.Contains(recipient+voice, forbidden) {
			t.Errorf("a preference is kept in browser storage (%s)", forbidden)
		}
	}
}
