package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
)

const (
	w10Original = "You idiot, the deploy broke prod again."
	w10Reworded = "The deploy broke prod again."
)

func w10Model(locale, author string) (Model, Message) {
	msg := Message{ID: "p1", Revision: 1, AuthorID: "dana", Author: "Dana", Body: w10Original, Sequence: 1}
	m := Model{Locale: locale, State: StateReady, SelectedID: "room", CurrentUser: author, CurrentTenantID: "tenant",
		ChatFeatures:  &ChatFeatures{Renderings: true},
		Conversations: []Conversation{{ID: "room", Kind: PublicChannel, Name: "General", Joined: true}},
		Messages:      []Message{msg}, ReaderSelections: map[string]ReaderSelection{}}
	return m, msg
}

// TestTodo_CHATTONE_002_WritersSide: after sending a message that is reworded
// for others, the writer sees "Others see:" with the wording and two actions,
// and the second only where the channel lets members read as written. Nobody
// else sees the block. In English, German and Arabic.
func TestTodo_CHATTONE_002_WritersSide(t *testing.T) {
	words := map[string]struct{ label, edit, send, pending, unavailable, written string }{
		"en-US": {"Others see:", "Edit wording", "Send as written", "Preparing the reworded version for others.", "The reworded version is not available, so others read your message as you wrote it.", "Others read your message as you wrote it."},
		"de-DE": {"Andere sehen:", "Wortlaut bearbeiten", "Wie geschrieben senden", "Die umformulierte Fassung für andere wird vorbereitet.", "Die umformulierte Fassung ist nicht verfügbar, daher lesen andere Ihre Nachricht so, wie Sie sie geschrieben haben.", "Andere lesen Ihre Nachricht so, wie Sie sie geschrieben haben."},
		"ar":    {"يرى الآخرون:", "عدّل الصياغة", "أرسلها كما كتبتها", "جارٍ إعداد النسخة المعاد صياغتها للآخرين.", "النسخة المعاد صياغتها غير متاحة، لذا يقرأ الآخرون رسالتك كما كتبتها.", "يقرأ الآخرون رسالتك كما كتبتها."},
	}
	for locale, want := range words {
		m, msg := w10Model(locale, "dana")
		m.Callbacks.BeginEdit, m.Callbacks.SetEditDraft = func(string) {}, func(string, string) {}
		sel := ReaderSelection{Revision: 1, Rendering: chatrender.Rendering{Message: "p1", Revision: 1, Tone: chatrender.AsWritten, Language: "en", SourceLanguage: "en", Text: w10Original},
			Mark: chatrender.Mark{State: "original", SourceLanguage: "en", CanShowOriginal: true, CanShowAsWritten: true}}

		sel.OthersSee = &ReaderOthersSee{State: "ready", Text: w10Reworded, CanSendAsWritten: true}
		m.ReaderSelections["p1"] = sel
		got := chatlangMessageMarkup(t, m, msg, localUI{})
		for _, text := range []string{want.label, want.edit, want.send, w10Reworded, `data-action="chatlang-reword-edit"`, `data-action="chatlang-reword-send"`} {
			if !strings.Contains(got, text) {
				t.Errorf("%s: the writer's block misses %q: %s", locale, text, got)
			}
		}
		// The writer's own text is what they wrote.
		if !strings.Contains(got, w10Original) {
			t.Errorf("%s: the writer no longer reads their own words", locale)
		}

		// Where the channel does not let members read as written there is no way to send so.
		sel.OthersSee = &ReaderOthersSee{State: "ready", Text: w10Reworded}
		m.ReaderSelections["p1"] = sel
		got = chatlangMessageMarkup(t, m, msg, localUI{})
		if strings.Contains(got, want.send) || !strings.Contains(got, want.edit) {
			t.Errorf("%s: Send as written is offered where originals are hidden, or Edit wording is missing", locale)
		}

		for state, text := range map[string]string{"pending": want.pending, "unavailable": want.unavailable, "written": want.written} {
			sel.OthersSee = &ReaderOthersSee{State: state}
			m.ReaderSelections["p1"] = sel
			got = chatlangMessageMarkup(t, m, msg, localUI{})
			if !strings.Contains(got, text) || strings.Contains(got, want.edit) || strings.Contains(got, want.label) {
				t.Errorf("%s: state %s: %s", locale, state, got)
			}
		}

		// A failed press says so and leaves the actions to try again.
		sel.OthersSee = &ReaderOthersSee{State: "ready", Text: w10Reworded, CanSendAsWritten: true}
		m.ReaderSelections["p1"] = sel
		failed := chatlangMessageMarkup(t, m, msg, localUI{chatlang: chatlangLocal{w10: chattoneW10Local{}.with("rw:p1", "failed")}})
		if !strings.Contains(failed, chattoneW10Text(locale, "send_failed")) || !strings.Contains(failed, want.send) {
			t.Errorf("%s: a failed press: %s", locale, failed)
		}

		// Another reader never sees the writer's block.
		other, _ := w10Model(locale, "eve")
		other.ReaderSelections["p1"] = sel
		if got := chatlangMessageMarkup(t, other, msg, localUI{}); strings.Contains(got, want.label) || strings.Contains(got, "chattone-others") {
			t.Errorf("%s: a reader sees the writer's block: %s", locale, got)
		}
	}
	// No block without the server's say-so.
	m, msg := w10Model("en-US", "dana")
	m.ReaderSelections["p1"] = ReaderSelection{Revision: 1, Rendering: chatrender.Rendering{Message: "p1", Revision: 1, Text: w10Original}, Mark: chatrender.Mark{State: "original"}}
	if got := chatlangMessageMarkup(t, m, msg, localUI{}); strings.Contains(got, "Others see:") {
		t.Fatalf("a block without a rewording: %s", got)
	}
}

// TestTodo_CHATTONE_003_ReaderMark: a reworded message has a mark in words, an
// accessible name that says so, and where allowed "Show as written" on that
// message alone, which reveals the original in place and "Show reworded" returns.
func TestTodo_CHATTONE_003_ReaderMark(t *testing.T) {
	words := map[string]struct{ name, mark, show, hide, asWritten string }{
		"en-US": {"Reworded message from Dana", "Reworded", "Show as written", "Show reworded", "As written"},
		"de-DE": {"Umformulierte Nachricht von Dana", "Umformuliert", "Wie geschrieben anzeigen", "Umformuliert anzeigen", "Wie geschrieben"},
		"ar":    {"رسالة معاد صياغتها من Dana", "أعيدت الصياغة", "عرض النص كما كُتب", "عرض النسخة المعاد صياغتها", "كما كُتب"},
	}
	for locale, want := range words {
		m, msg := w10Model(locale, "eve")
		reworded := ReaderSelection{Revision: 1,
			Rendering: chatrender.Rendering{Message: "p1", Revision: 1, Tone: chatrender.Reworded, Language: "en", SourceLanguage: "en", Text: w10Reworded, Kinds: []chatrender.Kind{chatrender.Reword}},
			Mark:      chatrender.Mark{State: "ready", Kinds: []chatrender.Kind{chatrender.Reword}, SourceLanguage: "en", CanShowOriginal: true, CanShowAsWritten: true}}
		m.ReaderSelections["p1"] = reworded
		got := chatlangMessageMarkup(t, m, msg, localUI{})
		for _, text := range []string{`aria-label="` + want.name + `"`, want.mark, want.show, w10Reworded, `data-chatlang-state="reworded"`} {
			if !strings.Contains(got, text) {
				t.Errorf("%s: a reworded message misses %q: %s", locale, text, got)
			}
		}
		if strings.Contains(got, w10Original) {
			t.Errorf("%s: the original is shown before it was asked for", locale)
		}
		opened := chatlangMessageMarkup(t, m, msg, localUI{chatlang: chatlangLocal{}.toggleOriginal("p1")})
		for _, text := range []string{w10Original, want.asWritten, want.hide, `aria-expanded="true"`} {
			if !strings.Contains(opened, text) {
				t.Errorf("%s: Show as written misses %q: %s", locale, text, opened)
			}
		}
		// Where the administrator does not allow originals there is no switch and
		// the original is in nothing the page draws.
		reworded.Mark.CanShowAsWritten, reworded.Mark.CanShowOriginal = false, false
		m.ReaderSelections["p1"] = reworded
		hidden := chatlangMessageMarkup(t, m, msg, localUI{chatlang: chatlangLocal{}.toggleOriginal("p1")})
		if strings.Contains(hidden, want.show) || strings.Contains(hidden, w10Original) || !strings.Contains(hidden, want.mark) {
			t.Errorf("%s: originals are hidden but the switch or the original is drawn: %s", locale, hidden)
		}
	}
	// A message read as written has no such name.
	m, msg := w10Model("en-US", "eve")
	if got := RenderingMessageLabel(m, msg); got != "" {
		t.Fatalf("a message without a selection has the name %q", got)
	}
}

// TestTodo_CHATLANG_006_ReaderMarks: the "may be inaccurate" words, the report
// form and the first-read notice, in English, German and Arabic.
func TestTodo_CHATLANG_006_ReaderMarks(t *testing.T) {
	words := map[string]struct{ inaccurate, report, prompt, send, notice, ok string }{
		"en-US": {"May be inaccurate", "Report this translation", "Suggest a better translation (optional)", "Send report", "Translated automatically.", "Got it"},
		"de-DE": {"Kann ungenau sein", "Diese Übersetzung melden", "Bessere Übersetzung vorschlagen (optional)", "Meldung senden", "Automatisch übersetzt.", "Verstanden"},
		"ar":    {"قد تكون غير دقيقة", "الإبلاغ عن هذه الترجمة", "اقترح ترجمة أفضل (اختياري)", "أرسل البلاغ", "تُرجمت تلقائياً.", "حسناً"},
	}
	for locale, want := range words {
		m, german, english, _ := chatlangModel(locale)
		sel := m.ReaderSelections["de"]
		sel.Inaccurate = true
		m.ReaderSelections["de"] = sel
		got := chatlangMessageMarkup(t, m, german, localUI{})
		for _, text := range []string{want.inaccurate, want.report, want.notice, want.ok, `data-chatlang-notice="first-read"`, `data-action="chatlang-report-open"`} {
			if !strings.Contains(got, text) {
				t.Errorf("%s: a translated message misses %q: %s", locale, text, got)
			}
		}
		// A pair that passed the higher mark carries no such words.
		sel.Inaccurate = false
		m.ReaderSelections["de"] = sel
		if got := chatlangMessageMarkup(t, m, german, localUI{}); strings.Contains(got, want.inaccurate) {
			t.Errorf("%s: a clean pair is marked inaccurate", locale)
		}
		// The notice is once: answered, it is gone; and a message left as written has none.
		answered := chatlangMessageMarkup(t, m, german, localUI{chatlang: chatlangLocal{w10: chattoneW10Local{noticeGone: true}}})
		if strings.Contains(answered, "chatlang-notice") {
			t.Errorf("%s: the notice shows again after it was answered", locale)
		}
		if plain := chatlangMessageMarkup(t, m, english, localUI{}); strings.Contains(plain, "chatlang-notice") || strings.Contains(plain, "chatlang-report-open") {
			t.Errorf("%s: a message left as written has the translation notice or report: %s", locale, plain)
		}
		// The report form.
		form := chatlangMessageMarkup(t, m, german, localUI{chatlang: chatlangLocal{w10: chattoneW10Local{}.openReport("de")}})
		for _, text := range []string{want.prompt, want.send, `id="chatlang-report-text-de"`, `data-action="chatlang-report-send"`} {
			if !strings.Contains(form, text) {
				t.Errorf("%s: the report form misses %q: %s", locale, text, form)
			}
		}
		sent := chatlangMessageMarkup(t, m, german, localUI{chatlang: chatlangLocal{w10: chattoneW10Local{reporting: "de"}.with("rp:de", "sent")}})
		if !strings.Contains(sent, chattoneW10Text(locale, "report_sent")) || strings.Contains(sent, `id="chatlang-report-text-de"`) {
			t.Errorf("%s: the sent report: %s", locale, sent)
		}
		// Writers do not report their own translation.
		m.CurrentUser = "hans"
		if own := chatlangMessageMarkup(t, m, german, localUI{}); strings.Contains(own, "chatlang-report-open") {
			t.Errorf("%s: the writer is offered a report of their own message", locale)
		}
	}
}

// TestTodo_CHATTONE_003_Copy: every key of the reword and report copy has all
// three languages and none prints a raw key.
func TestTodo_CHATTONE_003_WriterCopy(t *testing.T) {
	for key, row := range chattoneW10Copy {
		for index, locale := range []string{"en-US", "de-DE", "ar"} {
			if strings.TrimSpace(row[index]) == "" || chattoneW10Text(locale, key) != row[index] {
				t.Errorf("%s %s: %q", locale, key, row[index])
			}
		}
	}
}
