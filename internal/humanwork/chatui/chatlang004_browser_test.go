package chatui_test

import (
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// chatlangBrowserPage renders the whole Chat page with the product's own
// catalog (the one that answers an unknown key with ⟦key⟧) for a reader of one
// locale, in a conversation with a translated message, one whose translation is
// pending and one that could not be translated.
func chatlangBrowserPage(t *testing.T, locale, source, translation, original string, readers map[string]int) string {
	t.Helper()
	ctx := productui.ResolveProductLocale(locale)
	tag := chatrender.Language(ctx.Resolved)
	translated := chatui.Message{ID: "m1", Revision: 1, AuthorID: "hans", Author: "Hans", Body: original, Sequence: 1}
	waiting := chatui.Message{ID: "m2", Revision: 1, AuthorID: "mona", Author: "Mona", Body: original + " 2", Sequence: 2}
	failed := chatui.Message{ID: "m3", Revision: 1, AuthorID: "mona", Author: "Mona", Body: original + " 3", Sequence: 3}
	selections := map[string]chatui.ReaderSelection{
		"m1": {Revision: 1,
			Rendering: chatrender.Rendering{Message: "m1", Revision: 1, Tone: chatrender.AsWritten, Language: tag, SourceLanguage: source, Text: translation, Kinds: []chatrender.Kind{chatrender.Translate}},
			Mark:      chatrender.Mark{State: "ready", Kinds: []chatrender.Kind{chatrender.Translate}, SourceLanguage: source, CanShowOriginal: true, CanShowAsWritten: true}},
		"m2": {Revision: 1, Mark: chatrender.Mark{State: "pending", SourceLanguage: source, CanShowOriginal: true, Wanted: []chatrender.Kind{chatrender.Translate}}},
		"m3": {Revision: 1,
			Rendering: chatrender.Rendering{Message: "m3", Revision: 1, Tone: chatrender.AsWritten, Language: source, SourceLanguage: source, Text: failed.Body},
			Mark:      chatrender.Mark{State: "fallback", SourceLanguage: source, CanShowOriginal: true, Wanted: []chatrender.Kind{chatrender.Translate}}},
	}
	m := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), SelectedID: "room", CurrentUser: "eve", CurrentTenantID: "tenant",
		Text:             func(key string) string { return ctx.Text(key) },
		ChatFeatures:     &chatui.ChatFeatures{Renderings: true, Translating: true},
		Conversations:    []chatui.Conversation{{ID: "room", Kind: chatui.PublicChannel, Name: "general", Joined: true, MemberCount: 5}},
		Messages:         []chatui.Message{translated, waiting, failed},
		ReaderSelections: selections,
		Chatlang:         chatui.ChatlangModel{AudienceRoom: "room", Audience: chatui.ChatlangAudience{Language: tag, Offered: true, Readers: readers}},
		Callbacks:        chatui.Callbacks{SendMessage: func(string, string) {}}}
	page, err := ui.RenderToString(chatui.Build(m))
	if err != nil {
		t.Fatal(err)
	}
	return html.UnescapeString(page)
}

// TestTodo_CHATLANG_004_Browser renders the translated conversation with the
// real product catalog in en-US, de-DE and ar, and asserts the page prints
// every word of the feature in the reader's language and no copy key.
func TestTodo_CHATLANG_004_Browser(t *testing.T) {
	for _, c := range []struct {
		locale, source, original, translation string
		readers                               map[string]int
		words                                 []string
		dir                                   string
	}{
		{"en-US", "de", "Bitte lesen wir die Nachricht heute mit dem Team.", "Please read the message with the team today.", map[string]int{"de": 3},
			[]string{"Translated from German", "Show original", "Translating…", "Not translated", "Messages in other languages are translated for you.", "Show originals", "Language settings", "3 people will read this in German", "Translate messages into my language"}, "ltr"},
		{"de-DE", "en", "Please read the message with the team today.", "Bitte lesen Sie die Nachricht heute mit dem Team.", map[string]int{"en": 3},
			[]string{"Übersetzt aus Englisch", "Original anzeigen", "Wird übersetzt…", "Nicht übersetzt", "Nachrichten in anderen Sprachen werden für Sie übersetzt.", "Originale anzeigen", "Spracheinstellungen", "3 Personen lesen dies auf Englisch", "Nachrichten in meine Sprache übersetzen"}, "ltr"},
		{"ar", "en", "Please read the message with the team today.", "يرجى قراءة الرسالة مع الفريق اليوم.", map[string]int{"en": 3},
			[]string{"مترجم من الإنجليزية", "عرض الأصل", "جارٍ الترجمة…", "لم تتم الترجمة", "تُترجم الرسائل المكتوبة بلغات أخرى من أجلك.", "عرض النصوص الأصلية", "إعدادات اللغة", "سيقرأ 3 أشخاص هذه الرسالة بالإنجليزية", "ترجمة الرسائل إلى لغتي"}, "rtl"},
	} {
		t.Run(c.locale, func(t *testing.T) {
			page := chatlangBrowserPage(t, c.locale, c.source, c.translation, c.original, c.readers)
			if strings.Contains(page, "⟦") {
				t.Fatalf("the page prints a copy key: %s", regexp.MustCompile(`.{40}⟦[^⟧]*⟧`).FindString(page))
			}
			for _, want := range c.words {
				if !strings.Contains(page, want) {
					t.Errorf("the page misses %q", want)
				}
			}
			// The translation is what is read; the original is one press away, not shown.
			if !strings.Contains(page, c.translation) {
				t.Errorf("the page does not read as the translation")
			}
			if strings.Contains(page, `id="chatlang-original-m1"`) {
				t.Error("the original is shown before anyone asked")
			}
			// Each text carries its own language and direction.
			tag := chatrender.Language(c.locale)
			body := regexp.MustCompile(`<div[^>]*message-body[^>]*>`).FindString(page)
			if !strings.Contains(body, `lang="`+tag+`"`) || !strings.Contains(body, `dir="`+c.dir+`"`) {
				t.Errorf("the translated text does not carry its language and direction: %s", body)
			}
			// The pending and failed messages keep their text, with their own language.
			if !strings.Contains(page, c.original+" 2") || !strings.Contains(page, c.original+" 3") {
				t.Error("a message whose translation is pending or failed lost its text")
			}
			// The translation setting is not greyed out.
			if box := regexp.MustCompile(`<input[^>]*id="chatrender-translate"[^>]*>`).FindString(page); box == "" || strings.Contains(box, "disabled") {
				t.Errorf("the translation setting is missing or disabled: %q", box)
			}
			// A conversation whose readers all read the writer's language has no line.
			quiet := chatlangBrowserPage(t, c.locale, c.source, c.translation, c.original, map[string]int{})
			if strings.Contains(quiet, "chatlang-audience") {
				t.Error("the composer says something when nobody reads a translation")
			}
		})
	}
}
