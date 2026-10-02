package chatui

import (
	"strings"
	"testing"
	"time"
)

// chatbug079Stored is #general after a reload: the question was answered
// privately the day before, the record of the finished run is on the page, and
// the answer's text has not arrived yet.
func chatbug079Stored(locale string, due time.Time) Model {
	m := chat4Fixture(locale, "sent", false)
	m.PersonaActivityReady = true
	projection := &m.PersonaInvocations[0].Projection
	projection.Progress, projection.AnswerStored, projection.AnswerDue = nil, true, due
	return m
}

// The words a run at work is drawn with, in the three languages.
var chatbug079WorkingText = []string{"Finding an answer", "Still working", "Eine Antwort wird", "Wird noch bearbeitet", "جارٍ البحث عن إجابة", "ما زال العمل جاريًا"}

func chatbug079AssertNotWorking(t *testing.T, locale, when, page string) {
	t.Helper()
	for _, working := range chatbug079WorkingText {
		if strings.Contains(page, working) {
			t.Fatalf("%s, %s: a finished answer is drawn as a run at work (%q)", locale, when, working)
		}
	}
	for _, forbidden := range []string{`data-agent-reply-state="working"`, `data-agent-progress="true"`, "agent-working-dots", "agent-progress-cancel"} {
		if strings.Contains(page, forbidden) {
			t.Fatalf("%s, %s: a finished answer carries %s", locale, when, forbidden)
		}
	}
}

// A question whose private answer is stored never shows the working text: not
// before the agent activity arrives, not while the answer's text is on its way,
// not when that text fails to arrive, and not once it has.
func TestTodo_CHATBUG_079(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		first := chatbug040Asked(locale)
		chatbug079AssertNotWorking(t, locale, "before the activity arrived", render(t, first))

		waiting := render(t, chatbug079Stored(locale, time.Now().Add(time.Minute)))
		chatbug079AssertNotWorking(t, locale, "while the text is on its way", waiting)
		if got := strings.Count(waiting, `data-agent-reply-state="loading-answer"`); got != 1 {
			t.Fatalf("%s: %d placeholders hold the stored answer's room, want 1", locale, got)
		}
		if !strings.Contains(waiting, `data-reserved-for="question"`) {
			t.Fatalf("%s: the placeholder is not under the question it answers", locale)
		}

		overdue := render(t, chatbug079Stored(locale, time.Now().Add(-time.Second)))
		chatbug079AssertNotWorking(t, locale, "after the wait ended", overdue)
		if !strings.Contains(overdue, `data-agent-reply-state="answered-saved"`) || !strings.Contains(overdue, `href="`+ChannelReferenceURL("policy")+`"`) {
			t.Fatalf("%s: an answer whose text never arrived does not point at the saved copy", locale)
		}
		if strings.Contains(overdue, "{name}") || strings.Contains(overdue, "⟦") {
			t.Fatalf("%s: the saved-copy row shows a placeholder instead of copy", locale)
		}

		answered := chat4Fixture(locale, "answered", false)
		answered.PersonaActivityReady = true
		answered.PersonaInvocations[0].Projection.AnswerStored = true
		answered.PersonaInvocations[0].Projection.AnswerDue = time.Now().Add(-time.Hour)
		after := render(t, answered)
		chatbug079AssertNotWorking(t, locale, "with the answer on the page", after)
		if !strings.Contains(after, `data-agent-reply-state="answered-private"`) || strings.Contains(after, "loading-answer") || strings.Contains(after, "answered-saved") {
			t.Fatalf("%s: the answer did not replace its placeholder", locale)
		}
	}
	english := render(t, chatbug079Stored("en-US", time.Now().Add(-time.Second)))
	for _, want := range []string{"Policy Helper answered. The answer is saved in your conversation with Policy Helper.", "Open the answer", "Only visible to you"} {
		if !strings.Contains(english, want) {
			t.Fatalf("the saved-copy row does not read %q", want)
		}
	}
	// A run that really is in flight still says so.
	if working := render(t, chat4Fixture("en-US", "working2", false)); !strings.Contains(working, "Finding an answer in your policy documents") {
		t.Fatal("a run in flight no longer says it is working")
	}
	// In the agent's own conversation the answer is an ordinary message: nothing is held for it.
	direct := chat4Fixture("en-US", "sent", true)
	direct.PersonaInvocations[0].Projection.Progress, direct.PersonaInvocations[0].Projection.AnswerStored = nil, true
	if page := render(t, direct); strings.Contains(page, "loading-answer") || strings.Contains(page, "answered-saved") {
		t.Fatal("a direct conversation with an agent holds a placeholder for a stored answer")
	}
}

// The placeholder is hidden from assistive technology and the saved-copy row is
// a card like any other, in the reading direction of the page.
func TestTodo_CHATBUG_079_Browser(t *testing.T) {
	waiting := render(t, chatbug079Stored("ar", time.Now().Add(time.Minute)))
	if !strings.Contains(waiting, `aria-hidden="true"`) || !strings.Contains(waiting, `dir="rtl"`) {
		t.Fatal("the placeholder is exposed to assistive technology or ignores the page direction")
	}
	overdue := render(t, chatbug079Stored("de-DE", time.Now().Add(-time.Second)))
	for _, want := range []string{"Policy Helper hat geantwortet.", "Antwort öffnen", "Nur für Sie sichtbar"} {
		if !strings.Contains(overdue, want) {
			t.Fatalf("the German saved-copy row does not read %q", want)
		}
	}
}
