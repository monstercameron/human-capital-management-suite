package chatui

import (
	"strings"
	"testing"
)

func TestChatComposerKeepsMultilineDraftAheadOfActions(t *testing.T) {
	m := Model{
		State: StateReady, SelectedID: "room", Draft: "First line\nSecond line",
		Conversations: []Conversation{{ID: "room", Name: "People"}},
		Callbacks:     Callbacks{SendMessage: func(string, string) {}, DraftChanged: func(string, string) {}},
	}
	markup := render(t, m)
	input := strings.Index(markup, `id="chat-composer"`)
	toolbar := strings.Index(markup, `class="composer-toolbar"`)
	if input < 0 || toolbar <= input {
		t.Fatal("composer actions must follow the writing area")
	}
	if !strings.Contains(markup[input:toolbar], `rows="3"`) {
		t.Fatal("multiline writing area height was lost")
	}
	if !strings.Contains(markup[toolbar:], `class="send-button"`) || !strings.Contains(markup[toolbar:], `data-action="emoji-toggle"`) {
		t.Fatal("composer actions are missing")
	}
	if strings.Contains(Stylesheet, `.chat-composer .composer-toolbar{display:contents}`) || !strings.Contains(Stylesheet, `.chat-composer{position:relative;flex:none;min-width:0;`) {
		t.Fatal("composer must keep its writing area and toolbar in separate rows")
	}
}
