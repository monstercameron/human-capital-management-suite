package chatui

import (
	"strings"
	"testing"
)

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
		`id="chat-composer-emoji-picker"`,
		`id="thread-composer-emoji-picker"`,
		`role="dialog"`,
		`aria-label="Choose an emoji"`,
		` hidden`,
		`aria-label="Insert emoji"`,
		`data-action="emoji-insert"`,
		`data-id="chat-composer"`,
		`data-emoji="😀"`,
		`aria-label="Emoji 😀"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("rendered chat emoji picker is missing %q", want)
		}
	}
	if got := strings.Count(markup, `class="emoji-picker"`); got != 2 {
		t.Fatalf("rendered %d emoji pickers, want one for each composer", got)
	}
}

func TestChatEmojiPickerUsesLocalizedAccessibleLabels(t *testing.T) {
	model := Model{State: StateReady, SelectedID: "room", Conversations: []Conversation{{ID: "room", Name: "People"}}, Text: func(key string) string {
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
	for _, want := range []string{`aria-label="Emoji einfügen"`, `aria-label="Emoji auswählen"`, `aria-label="Emoji 😀"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("localized emoji picker is missing %q", want)
		}
	}
}
