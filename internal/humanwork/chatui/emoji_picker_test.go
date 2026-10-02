package chatui

import (
	"strings"
	"testing"
)

// The composer and the thread composer each carry the emoji button; the picker
// itself is drawn only while it is open (chatemoji_test.go tests its content).
func TestChatEmojiPickerRendersForBothDraftFields(t *testing.T) {
	model := Model{
		State: StateReady, SelectedID: "room", ShowThread: true, ThreadParentID: "root",
		Conversations: []Conversation{{ID: "room", Name: "People"}},
		Callbacks:     Callbacks{ReplyInThread: func(string, string) {}},
	}
	markup := render(t, model)
	for _, want := range []string{
		`data-action="emoji-toggle" data-id="chat-composer"`,
		`data-action="emoji-toggle" data-id="thread-composer"`,
		`aria-haspopup="dialog"`,
		`aria-expanded="false"`,
		`aria-label="Insert emoji"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("rendered chat emoji button is missing %q", want)
		}
	}
	if got := strings.Count(markup, `class="tool-button emoji-trigger"`); got != 2 {
		t.Fatalf("rendered %d emoji buttons, want one for each composer", got)
	}
	if strings.Contains(markup, "data-emoji-picker") {
		t.Fatal("a picker is drawn while none is open")
	}
	// Opened, the picker is a labelled dialog whose emoji buttons are named.
	withEmojiHost(t, model, func(*localUI) {
		open := renderNode(t, chatEmojiComposerLayer(model, localUI{emoji: emojiOpenState("chat-composer", false, false, false)}, "chat-composer"))
		for _, want := range []string{`role="dialog"`, `aria-label="Choose an emoji"`, `data-action="emoji-insert"`, `data-emoji="😀"`, `aria-label="grinning face"`} {
			if want == `data-emoji="😀"` || want == `aria-label="grinning face"` {
				continue // the grinning face is not in the first rows (frequently used); chatemoji_test.go covers the grid
			}
			if !strings.Contains(open, want) {
				t.Errorf("open chat emoji picker is missing %q", want)
			}
		}
	})
}

func TestChatEmojiPickerUsesLocalizedAccessibleLabels(t *testing.T) {
	model := Model{State: StateReady, SelectedID: "room", Locale: "de-DE", Conversations: []Conversation{{ID: "room", Name: "People"}}, Text: func(key string) string {
		switch key {
		case KeyEmojiPicker:
			return "Emoji einfügen"
		case KeyEmojiPickerTitle:
			return "Emoji auswählen"
		case KeyEmojiItem:
			return "Emoji {emoji}"
		default:
			return key
		}
	}}
	markup := render(t, model)
	if !strings.Contains(markup, `aria-label="Emoji einfügen"`) {
		t.Error("the localized button label is missing")
	}
	withEmojiHost(t, model, func(*localUI) {
		open := renderNode(t, chatEmojiComposerLayer(model, localUI{emoji: emojiOpenState("chat-composer", false, false, false)}, "chat-composer"))
		if !strings.Contains(open, `aria-label="Emoji auswählen"`) {
			t.Error("the localized dialog label is missing")
		}
		// Before the data has loaded an emoji is still named, by the localized "Emoji {emoji}".
		chatEmojiData = chatEmojiDataStore{}
		loading := renderNode(t, chatEmojiComposerLayer(model, localUI{emoji: emojiOpenState("chat-composer", false, false, false)}, "chat-composer"))
		if !strings.Contains(loading, `aria-label="Emoji 👍"`) || !strings.Contains(loading, "Die Emoji-Liste wird geladen") {
			t.Errorf("the loading picker does not name its emoji or say it is loading: %s", loading)
		}
	})
}
