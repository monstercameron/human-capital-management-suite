package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
)

func issue3814Model(locale string) (Model, Message) {
	msg := Message{ID: "de", Revision: 1, AuthorID: "hans", Author: "Hans", Body: chatlangGerman, Sequence: 1}
	left := ReaderSelection{Revision: 1,
		Rendering: chatrender.Rendering{Message: "de", Revision: 1, Tone: chatrender.AsWritten, Language: "de", SourceLanguage: "de", Text: chatlangGerman},
		Mark:      chatrender.Mark{State: "original", SourceLanguage: "de", CanShowOriginal: true, CanShowAsWritten: true}}
	m := Model{Locale: locale, State: StateReady, SelectedID: "room", CurrentUser: "eve", CurrentTenantID: "tenant",
		ChatFeatures:     &ChatFeatures{Renderings: true, Translating: true},
		Conversations:    []Conversation{{ID: "room", Kind: PublicChannel, Name: "General", Joined: true}},
		Messages:         []Message{msg},
		ReaderSelections: map[string]ReaderSelection{"de": left}}
	return m, msg
}

// TestIssue3814_TranslateMenuItem: someone else's message offers Translate in
// en-US, de-DE and ar; the writer's own message, an empty body, a conversation
// without renderings, a message already translated, and one the server refused
// do not.
func TestIssue3814_TranslateMenuItem(t *testing.T) {
	for locale, want := range map[string]string{"en-US": "Translate", "de-DE": "Übersetzen", "ar": "ترجمة"} {
		m, msg := issue3814Model(locale)
		menu := renderNode(t, spanOf(chatux022MenuItems(m, msg)))
		if !strings.Contains(menu, `data-action="chatlang-translate"`) || !strings.Contains(menu, want) {
			t.Errorf("%s: the menu has no Translate entry (%q): %s", locale, want, menu)
		}
	}
	has := func(m Model, msg Message) bool {
		return strings.Contains(renderNode(t, spanOf(chatux022MenuItems(m, msg))), `data-action="chatlang-translate"`)
	}
	m, msg := issue3814Model("en-US")
	own := msg
	own.AuthorID = "eve"
	if has(m, own) {
		t.Error("a person's own message must not offer Translate")
	}
	empty := msg
	empty.Body = "  "
	if has(m, empty) {
		t.Error("a message without text must not offer Translate")
	}
	off := m
	off.ChatFeatures = &ChatFeatures{}
	if has(off, msg) {
		t.Error("a conversation without renderings must not offer Translate")
	}
	done := m
	done.ReaderSelections = map[string]ReaderSelection{"de": {Revision: 1, Mark: chatrender.Mark{State: "ready", Kinds: []chatrender.Kind{chatrender.Translate}, SourceLanguage: "de", CanShowOriginal: true}, Rendering: chatrender.Rendering{Message: "de", Revision: 1, Language: "en", Text: chatlangEnglish}}}
	if has(done, msg) {
		t.Error("an already translated message must not offer Translate again")
	}
	refused := m
	refused.ReaderSelections = map[string]ReaderSelection{"de": {Revision: 1, Mark: chatrender.Mark{State: "original", SourceLanguage: "de", CanShowOriginal: true, Refused: chatrender.RefusedPolicy}}}
	if has(refused, msg) {
		t.Error("a message the server refused to translate must not offer Translate")
	}
}

// TestIssue3814_TranslateStatusNotes: after the press the message says
// "Translating…" until the server answers, then a refusal in words in the
// reader's language; the translation itself carries the existing mark and
// "Show original".
func TestIssue3814_TranslateStatusNotes(t *testing.T) {
	t.Cleanup(chatlangTranslateReset)
	m, msg := issue3814Model("en-US")
	if got := chatlangMessageMarkup(t, m, msg, localUI{}); strings.Contains(got, "chatlang-note") {
		t.Fatalf("a message nobody asked about carries a note: %s", got)
	}
	chatlangTranslateAsk("de")
	if got := chatlangMessageMarkup(t, m, msg, localUI{}); !strings.Contains(got, `data-chatlang-note="pending"`) || !strings.Contains(got, "Translating…") || !strings.Contains(got, `role="status"`) {
		t.Fatalf("no progress note after the press: %s", got)
	}
	for locale, want := range map[string]string{"en-US": "be translated here", "de-DE": "nicht übersetzt werden", "ar": "لا يمكن ترجمة"} {
		r, _ := issue3814Model(locale)
		r.ReaderSelections = map[string]ReaderSelection{"de": {Revision: 1, Mark: chatrender.Mark{State: "original", SourceLanguage: "de", CanShowOriginal: true, Refused: chatrender.RefusedPolicy}}}
		if got := chatlangMessageMarkup(t, r, msg, localUI{}); !strings.Contains(got, `data-chatlang-note="refused"`) || !strings.Contains(got, want) {
			t.Errorf("%s: refusal is not in words (%q): %s", locale, want, got)
		}
	}
	same, _ := issue3814Model("en-US")
	same.ReaderSelections = map[string]ReaderSelection{"de": {Revision: 1, Mark: chatrender.Mark{State: "original", SourceLanguage: "de", CanShowOriginal: true, Refused: chatrender.RefusedSameLanguage}}}
	if got := chatlangMessageMarkup(t, same, msg, localUI{}); !strings.Contains(got, "already in a language you read") {
		t.Errorf("same-language answer is not in words: %s", got)
	}
	translated, _ := issue3814Model("en-US")
	translated.ReaderSelections = map[string]ReaderSelection{"de": {Revision: 1, Mark: chatrender.Mark{State: "ready", Kinds: []chatrender.Kind{chatrender.Translate}, SourceLanguage: "de", CanShowOriginal: true},
		Rendering: chatrender.Rendering{Message: "de", Revision: 1, Language: "en", SourceLanguage: "de", Text: chatlangEnglish}}}
	got := chatlangMessageMarkup(t, translated, msg, localUI{})
	if strings.Contains(got, "chatlang-note") || !strings.Contains(got, "Translated from German") || !strings.Contains(got, "Show original") || !strings.Contains(got, chatlangEnglish) {
		t.Errorf("a translated message must show the mark and the original toggle, not a note: %s", got)
	}
	if ids := ChatlangTranslateAskedAmong([]string{"x", "de", "y"}); len(ids) != 1 || ids[0] != "de" {
		t.Errorf("asked ids = %v", ids)
	}
}
