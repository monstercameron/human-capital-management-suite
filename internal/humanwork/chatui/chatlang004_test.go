package chatui

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
)

const (
	chatlangGerman  = "Bitte lesen wir die Nachricht heute mit dem Team."
	chatlangEnglish = "Please read the message with the team today."
	chatlangArabic  = "مرحبًا بكم في هذه المحادثة اليوم مع جميع الزملاء"
)

// chatlangModel is a conversation of three messages as the server's selection
// for an English reader describes them: a German message translated into
// English, an English message that is left as it is, and an Arabic message
// whose translation is still on its way.
func chatlangModel(locale string) (Model, Message, Message, Message) {
	german := Message{ID: "de", Revision: 1, AuthorID: "hans", Author: "Hans", Body: chatlangGerman, Sequence: 1}
	english := Message{ID: "en", Revision: 1, AuthorID: "eve", Author: "Eve", Body: chatlangEnglish, Sequence: 2}
	arabic := Message{ID: "ar", Revision: 1, AuthorID: "mona", Author: "Mona", Body: chatlangArabic, Sequence: 3}
	translated := ReaderSelection{Revision: 1,
		Rendering: chatrender.Rendering{Message: "de", Revision: 1, Tone: chatrender.AsWritten, Language: "en", SourceLanguage: "de", Text: chatlangEnglish, Kinds: []chatrender.Kind{chatrender.Translate}},
		Mark:      chatrender.Mark{State: "ready", Kinds: []chatrender.Kind{chatrender.Translate}, SourceLanguage: "de", CanShowOriginal: true, CanShowAsWritten: true}}
	left := ReaderSelection{Revision: 1,
		Rendering: chatrender.Rendering{Message: "en", Revision: 1, Tone: chatrender.AsWritten, Language: "en", SourceLanguage: "en", Text: chatlangEnglish},
		Mark:      chatrender.Mark{State: "original", SourceLanguage: "en", CanShowOriginal: true, CanShowAsWritten: true}}
	pending := ReaderSelection{Revision: 1, Mark: chatrender.Mark{State: "pending", SourceLanguage: "ar", CanShowOriginal: true, CanShowAsWritten: true, Wanted: []chatrender.Kind{chatrender.Translate}}}
	m := Model{Locale: locale, State: StateReady, SelectedID: "room", CurrentUser: "eve", CurrentTenantID: "tenant", ReaderPending: true,
		ChatFeatures:     &ChatFeatures{Renderings: true, Translating: true},
		Conversations:    []Conversation{{ID: "room", Kind: PublicChannel, Name: "General", Joined: true}},
		Messages:         []Message{german, english, arabic},
		ReaderSelections: map[string]ReaderSelection{"de": translated, "en": left, "ar": pending}}
	return m, german, english, arabic
}

func chatlangMessageMarkup(t *testing.T, m Model, msg Message, local localUI) string {
	t.Helper()
	return renderNode(t, message(m, handlers{local: local}, msg, false))
}

// TestTodo_CHATLANG_004: a translated message reads like any other with a mark
// in words and the original one press away; a message left as written shows no
// mark; a pending translation leaves the text as written and says so.
func TestTodo_CHATLANG_004(t *testing.T) {
	m, german, english, arabic := chatlangModel("en-US")

	got := chatlangMessageMarkup(t, m, german, localUI{})
	if !strings.Contains(got, chatlangEnglish) || strings.Contains(got, chatlangGerman) {
		t.Fatalf("the translation is not what is read, or the original is already shown: %s", got)
	}
	for _, want := range []string{"Translated from German", "Show original", `lang="en"`, `dir="ltr"`, `data-chatlang-state="translated"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("a translated message misses %q: %s", want, got)
		}
	}

	// Show original: the original beneath the translation, marked with its own
	// language and direction, and the button now offers to hide it.
	opened := chatlangMessageMarkup(t, m, german, localUI{chatlang: chatlangLocal{}.toggleOriginal("de")})
	for _, want := range []string{chatlangEnglish, chatlangGerman, "Original (German)", "Hide original", `lang="de"`, `aria-expanded="true"`} {
		if !strings.Contains(opened, want) {
			t.Fatalf("the original is not shown beneath the translation (%q): %s", want, opened)
		}
	}
	if strings.Index(opened, chatlangEnglish) > strings.Index(opened, chatlangGerman) {
		t.Fatal("the original must come beneath the translation")
	}
	// The conversation switch shows every original.
	all := chatlangMessageMarkup(t, m, german, localUI{chatlang: chatlangLocal{}.toggleAll("room")})
	if !strings.Contains(all, chatlangGerman) || !strings.Contains(all, "Hide original") {
		t.Fatalf("Show originals did not show the original: %s", all)
	}
	if other := chatlangMessageMarkup(t, m, german, localUI{chatlang: chatlangLocal{}.toggleAll("another-room")}); strings.Contains(other, chatlangGerman) {
		t.Fatal("the switch of another conversation opened this one")
	}

	// A message left as written carries no mark and no button; its text has its
	// own language.
	plain := chatlangMessageMarkup(t, m, english, localUI{})
	if strings.Contains(plain, "chatlang-mark") || strings.Contains(plain, "Show original") || !strings.Contains(plain, `lang="en"`) {
		t.Fatalf("a message left as written has a mark or no language: %s", plain)
	}

	// Pending: the text as written stays, with a small "Translating…".
	waiting := chatlangMessageMarkup(t, m, arabic, localUI{})
	if !strings.Contains(waiting, chatlangArabic) || !strings.Contains(waiting, "Translating…") || strings.Contains(waiting, "Show original") || !strings.Contains(waiting, `dir="rtl"`) || !strings.Contains(waiting, `lang="ar"`) {
		t.Fatalf("a pending translation: %s", waiting)
	}

	// Could not translate: the original with "Not translated", never an error in
	// place of the text.
	m.ReaderSelections["ar"] = ReaderSelection{Revision: 1,
		Rendering: chatrender.Rendering{Message: "ar", Revision: 1, Tone: chatrender.AsWritten, Language: "ar", SourceLanguage: "ar", Text: chatlangArabic},
		Mark:      chatrender.Mark{State: "fallback", SourceLanguage: "ar", CanShowOriginal: true, Wanted: []chatrender.Kind{chatrender.Translate}}}
	failed := chatlangMessageMarkup(t, m, arabic, localUI{})
	if !strings.Contains(failed, chatlangArabic) || !strings.Contains(failed, "Not translated") || strings.Contains(failed, "not ready") || strings.Contains(failed, "error") {
		t.Fatalf("a failed translation: %s", failed)
	}

	// Reworded and translated: the reader has the text in their language and both marks.
	both := m.ReaderSelections["de"]
	both.Rendering.Kinds = []chatrender.Kind{chatrender.Reword, chatrender.Translate}
	both.Mark.Kinds = both.Rendering.Kinds
	m.ReaderSelections["de"] = both
	if got := chatlangMessageMarkup(t, m, german, localUI{}); !strings.Contains(got, "Reworded") || !strings.Contains(got, "Translated from German") {
		t.Fatalf("a reworded and translated message misses a mark: %s", got)
	}
	both.Rendering.Kinds, both.Mark.Kinds = []chatrender.Kind{chatrender.Translate}, []chatrender.Kind{chatrender.Translate}
	m.ReaderSelections["de"] = both

	// The conversation bar: the switch and the way to the language settings.
	bar := renderNode(t, chatlangBar(m, handlers{}))
	for _, want := range []string{"Messages in other languages are translated for you.", "Show originals", `aria-pressed="false"`, "Language settings", `data-action="chatlang-settings"`} {
		if !strings.Contains(bar, want) {
			t.Fatalf("the conversation bar misses %q: %s", want, bar)
		}
	}
	if on := renderNode(t, chatlangBar(m, handlers{local: localUI{chatlang: chatlangLocal{}.toggleAll("room")}})); !strings.Contains(on, `aria-pressed="true"`) {
		t.Fatalf("the switch does not say it is on: %s", on)
	}
	// A conversation with nothing translated has no bar.
	quiet := m
	quiet.ReaderSelections = map[string]ReaderSelection{"en": m.ReaderSelections["en"]}
	if chatlangBar(quiet, handlers{}) != nil || chatlangBar(Model{}, handlers{}) != nil {
		t.Fatal("a conversation in one language shows the translation bar")
	}
}

// TestTodo_CHATLANG_004_Composer: one muted line saying who will read a
// translation, and nothing when everyone reads the writer's language.
func TestTodo_CHATLANG_004_Composer(t *testing.T) {
	m, _, _, _ := chatlangModel("en-US")
	m.Chatlang = ChatlangModel{AudienceRoom: "room", Audience: ChatlangAudience{Language: "en", Offered: true, Readers: map[string]int{"de": 3}}}
	line := renderNode(t, chatlangComposerLine(m))
	if !strings.Contains(line, "3 people will read this in German") {
		t.Fatalf("composer line: %s", line)
	}
	for locale, want := range map[string]string{
		"de-DE": "3 Personen lesen dies auf Deutsch",
		"ar":    "سيقرأ 3 أشخاص هذه الرسالة بالألمانية",
	} {
		m.Locale = locale
		if got := renderNode(t, chatlangComposerLine(m)); !strings.Contains(got, want) {
			t.Fatalf("%s: %s", locale, got)
		}
	}
	m.Locale = "en-US"
	for audience, want := range map[string]string{
		"one":    "1 person will read this in German",
		"two":    "5 people will read this in German and Arabic",
		"three":  "6 people will read this in German, Arabic and Spanish",
		"nobody": "",
	} {
		readers := map[string]int{"de": 1}
		switch audience {
		case "two":
			readers = map[string]int{"de": 3, "ar": 2}
		case "three":
			readers = map[string]int{"de": 3, "ar": 2, "es": 1}
		case "nobody":
			readers = map[string]int{}
		}
		if got := ChatlangAudienceLine(m, ChatlangAudience{Offered: true, Readers: readers}); got != want {
			t.Fatalf("%s: %q, want %q", audience, got, want)
		}
	}
	// Nothing when translation is not on, when the answer is for another
	// conversation, when it is not known yet, or when the feature is off.
	for name, mutate := range map[string]func(*Model){
		"not offered":     func(m *Model) { m.Chatlang.Audience.Offered = false },
		"other room":      func(m *Model) { m.Chatlang.AudienceRoom = "elsewhere" },
		"not read":        func(m *Model) { m.Chatlang = ChatlangModel{} },
		"feature off":     func(m *Model) { m.ChatFeatures = &ChatFeatures{} },
		"no features":     func(m *Model) { m.ChatFeatures = nil },
		"zero readers":    func(m *Model) { m.Chatlang.Audience.Readers = map[string]int{"de": 0} },
		"no conversation": func(m *Model) { m.SelectedID = "" },
	} {
		again := m
		mutate(&again)
		if chatlangComposerLine(again) != nil {
			t.Fatalf("%s: the composer shows a line", name)
		}
	}
	// The line is part of the composer.
	m.Draft = ""
	page := renderNode(t, composer(m, handlers{}))
	if !strings.Contains(page, "chatlang-audience") || !strings.Contains(page, "3 people will read this in German") {
		t.Fatalf("the composer does not carry the line: %s", page)
	}
}

// TestTodo_CHATLANG_004_Accessibility: each text carries its own language and
// direction, the buttons are named and say what they open, and nothing is a
// live region that would read every message out again.
func TestTodo_CHATLANG_004_Accessibility(t *testing.T) {
	m, german, _, _ := chatlangModel("en-US")
	got := chatlangMessageMarkup(t, m, german, localUI{chatlang: chatlangLocal{}.toggleOriginal("de")})
	button := regexp.MustCompile(`<button[^>]*chatlang-mark-button[^>]*>`).FindString(got)
	for _, want := range []string{`aria-label="Hide original of the message from Hans"`, `aria-expanded="true"`, `aria-controls="chatlang-original-de"`, `type="button"`} {
		if !strings.Contains(button, want) {
			t.Fatalf("the original button misses %q: %s", want, button)
		}
	}
	if !strings.Contains(got, `id="chatlang-original-de"`) {
		t.Fatalf("the button controls an element that is not there: %s", got)
	}
	if strings.Contains(got, `role="status"`) || strings.Contains(got, `aria-live`) {
		t.Fatalf("a translated message announces itself: %s", got)
	}
	// The original and the translation are each marked with their language.
	body := regexp.MustCompile(`<div[^>]*message-body[^>]*>`).FindString(got)
	original := regexp.MustCompile(`<div[^>]*chatlang-original-text[^>]*>`).FindString(got)
	if !strings.Contains(body, `lang="en"`) || !strings.Contains(original, `lang="de"`) || !strings.Contains(original, `dir="ltr"`) {
		t.Fatalf("language marks: body %s original %s", body, original)
	}
	// Arabic reads right to left in a left-to-right page, and the other way round.
	arabic := Message{ID: "ar2", Revision: 1, AuthorID: "mona", Author: "Mona", Body: chatlangArabic}
	m.ReaderSelections["ar2"] = ReaderSelection{Revision: 1, Rendering: chatrender.Rendering{Message: "ar2", Revision: 1, Tone: chatrender.AsWritten, Language: "ar", SourceLanguage: "ar", Text: chatlangArabic}, Mark: chatrender.Mark{State: "original", SourceLanguage: "ar", CanShowOriginal: true}}
	rtl := regexp.MustCompile(`<div[^>]*message-body[^>]*>`).FindString(chatlangMessageMarkup(t, m, arabic, localUI{}))
	if !strings.Contains(rtl, `dir="rtl"`) || !strings.Contains(rtl, `lang="ar"`) {
		t.Fatalf("an Arabic message in an English page: %s", rtl)
	}
	m.Locale = "ar"
	m.Direction = "rtl"
	latin := regexp.MustCompile(`<div[^>]*message-body[^>]*>`).FindString(chatlangMessageMarkup(t, m, Message{ID: "en", Revision: 1, AuthorID: "eve", Author: "Eve", Body: chatlangEnglish}, localUI{}))
	if !strings.Contains(latin, `dir="ltr"`) || !strings.Contains(latin, `lang="en"`) {
		t.Fatalf("an English message in an Arabic page: %s", latin)
	}
	// A message whose language is not known keeps automatic direction.
	m.ReaderSelections["und"] = ReaderSelection{Revision: 1, Rendering: chatrender.Rendering{Message: "und", Revision: 1, Tone: chatrender.AsWritten, Language: "und", SourceLanguage: "und", Text: "ok"}, Mark: chatrender.Mark{State: "original", SourceLanguage: "und", CanShowOriginal: true}}
	unknown := regexp.MustCompile(`<div[^>]*message-body[^>]*>`).FindString(chatlangMessageMarkup(t, m, Message{ID: "und", Revision: 1, AuthorID: "eve", Author: "Eve", Body: "ok"}, localUI{}))
	if !strings.Contains(unknown, `dir="auto"`) || strings.Contains(unknown, `lang=`) {
		t.Fatalf("a message with no language: %s", unknown)
	}
	// Names, links, mentions and code are laid out on their own inside either
	// direction: the stylesheet isolates them and keeps code left to right.
	for _, rule := range []string{`.message-body[dir=rtl],.message-body[dir=ltr]{unicode-bidi:isolate}`, `.message-body :is(code,pre){direction:ltr;unicode-bidi:isolate;text-align:left}`, `.message-body :is(a,time,.mention-chip,.mention-chip-details){unicode-bidi:isolate}`} {
		if !strings.Contains(Stylesheet, rule) {
			t.Fatalf("the page stylesheet misses %q", rule)
		}
	}
	// The mark is not clipped at 320 px and is not a live region.
	if strings.Contains(ChatlangStyles, "white-space:nowrap") {
		t.Fatal("the mark cannot wrap at narrow widths")
	}
}

// TestTodo_CHATLANG_004_Security: "Show original" never shows more than the
// server allowed this reader, whatever the page state says.
func TestTodo_CHATLANG_004_Security(t *testing.T) {
	m, german, _, _ := chatlangModel("en-US")
	everything := localUI{chatlang: chatlangLocal{}.toggleAll("room").toggleOriginal("de")}

	// The original is withheld: no button and no original, even with the
	// switches on.
	selection := m.ReaderSelections["de"]
	selection.Mark.CanShowOriginal = false
	m.ReaderSelections["de"] = selection
	got := chatlangMessageMarkup(t, m, german, everything)
	if strings.Contains(got, chatlangGerman) || strings.Contains(got, "Show original") || strings.Contains(got, "Hide original") || strings.Contains(got, "chatlang-original") {
		t.Fatalf("a withheld original was shown: %s", got)
	}
	if !strings.Contains(got, chatlangEnglish) || !strings.Contains(got, "Translated from German") {
		t.Fatalf("the translation went with it: %s", got)
	}

	// A selection for another revision (the message was edited) is not used.
	selection.Mark.CanShowOriginal = true
	m.ReaderSelections["de"] = selection
	edited := german
	edited.Revision = 2
	edited.Body = "Neuer Text, bitte lesen."
	stale := chatlangMessageMarkup(t, m, edited, everything)
	if strings.Contains(stale, chatlangEnglish) || strings.Contains(stale, "Translated from") || strings.Contains(stale, "Show original") {
		t.Fatalf("a translation of the old revision was used: %s", stale)
	}
	// A selection for another message is not used either.
	m.ReaderSelections["other"] = m.ReaderSelections["de"]
	if view := chatlangMessageOf(m, Message{ID: "other", Revision: 1, Body: "x"}); view.Known {
		t.Fatalf("a selection naming another message was used: %+v", view)
	}

	// Translated text is text: markup in it is escaped, never run.
	hostile := m
	hostile.ReaderSelections = map[string]ReaderSelection{"de": m.ReaderSelections["de"]}
	hostileSelection := hostile.ReaderSelections["de"]
	hostileSelection.Rendering.Text = `<img src=x onerror=alert(1)> <script>alert(2)</script>`
	hostile.ReaderSelections["de"] = hostileSelection
	page := chatlangMessageMarkup(t, hostile, german, localUI{})
	if strings.Contains(page, "<img") || strings.Contains(page, "<script") {
		t.Fatalf("translated text was rendered as markup: %s", page)
	}

	// An agent's message is not given a translation mark by this view.
	agent := german
	agent.PersonaActor = &PersonaActor{}
	if chatlangMessageOf(m, agent).Known {
		t.Fatal("an agent's message was treated as a translated human message")
	}
	// The writer's picker is the writer's: nobody else sees it, even if the
	// state says it is open for their message.
	other := m
	other.CurrentUser = "someone-else"
	if picker := chatlangMessageMarkup(t, other, german, localUI{chatlang: chatlangLocal{}.openFixer("de")}); strings.Contains(picker, "chatlang-fix") {
		t.Fatalf("another person was offered the language picker: %s", picker)
	}
	if len(chatlangMenuItems(other, german)) != 0 {
		t.Fatal("another person's More menu offers the language of a message that is not theirs")
	}
}

// TestTodo_CHATLANG_004_Writer: the writer corrects the language of their own
// message, and the page state follows the answer.
func TestTodo_CHATLANG_004_Writer(t *testing.T) {
	m, german, english, _ := chatlangModel("en-US")
	m.CurrentUser = "hans"
	if items := chatlangMenuItems(m, german); len(items) != 1 {
		t.Fatalf("the writer's menu has %d language entries", len(items))
	}
	if len(chatlangMenuItems(m, english)) != 0 {
		t.Fatal("the menu of another person's message offers a language change")
	}
	menu := renderNode(t, html.Div(html.Props{}, chatlangMenuItems(m, german)...))
	if !strings.Contains(menu, "Message language…") || !strings.Contains(menu, `data-action="chatlang-fix"`) {
		t.Fatalf("menu entry: %s", menu)
	}
	// The menu of a message the server has not answered for offers nothing.
	if len(chatlangMenuItems(m, Message{ID: "unknown", AuthorID: "hans", Revision: 1})) != 0 {
		t.Fatal("the menu offers a language before the server has detected one")
	}

	open := chatlangLocal{}.openFixer("de")
	picker := chatlangMessageMarkup(t, m, german, localUI{chatlang: open})
	for _, want := range []string{"Language of this message", `data-action="chatlang-correct"`, `data-extra="de"`, `data-extra="und"`, "No language", `data-action="chatlang-fix-cancel"`, "Cancel"} {
		if !strings.Contains(picker, want) {
			t.Fatalf("the picker misses %q: %s", want, picker)
		}
	}
	if !regexp.MustCompile(`aria-pressed="true"[^>]*data-extra="de"|data-extra="de"[^>]*aria-pressed="true"`).MatchString(picker) {
		t.Fatalf("the detected language is not marked: %s", picker)
	}
	busy := renderNode(t, chatlangFixer(m, open.saving(), german, chatlangMessageOf(m, german)))
	if !strings.Contains(busy, "Changing the language…") || !strings.Contains(busy, "disabled") {
		t.Fatalf("saving: %s", busy)
	}
	failed := open.saving().saved(errors.New("nope"))
	if !failed.failed || failed.busy || failed.fixing != "de" {
		t.Fatalf("a failed correction closed the picker or hid the failure: %+v", failed)
	}
	if text := renderNode(t, chatlangFixer(m, failed, german, chatlangMessageOf(m, german))); !strings.Contains(text, "The language could not be changed. Try again.") || strings.Contains(text, "Changing the language") {
		t.Fatalf("failure: %s", text)
	}
	if done := failed.saving().saved(nil); done.fixing != "" || done.failed || done.busy {
		t.Fatalf("a correction that was accepted left the picker open: %+v", done)
	}

	// Pure state: each press is one step and never changes the other messages.
	l := chatlangLocal{}.toggleOriginal("a").toggleOriginal("b").toggleOriginal("a")
	if l.original["a"] || !l.original["b"] || !l.toggleAll("r").all["r"] || l.toggleAll("r").toggleAll("r").all["r"] {
		t.Fatalf("toggles: %+v", l)
	}
	if !l.originalShown("r", "b") || l.originalShown("r", "a") || !l.toggleAll("r").originalShown("r", "a") {
		t.Fatal("originalShown")
	}
	if revision, ok := chatlangMessageRevision(m, "de"); !ok || revision != 1 {
		t.Fatal("revision of the message the writer corrects")
	}
	if _, ok := chatlangMessageRevision(m, "missing"); ok {
		t.Fatal("a revision for a message that is not shown")
	}
}
