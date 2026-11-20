package chatui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func issue3808Message(t *testing.T, audience string) Message {
	t.Helper()
	body, err := chat.ClockCardBody(chat.ClockCard{Kind: chat.ClockCardMissingPunch, Audience: audience, Subject: "maria", Day: "2026-11-17", Ref: "sess-1"})
	if err != nil {
		t.Fatal(err)
	}
	return Message{ID: "card", AuthorID: "system:time", Author: "Time", Body: body, TimeLabel: "9:41"}
}

func issue3808Model() Model {
	m := chatux021Model()
	m.CurrentUser = "maria"
	m.Members = append(m.Members, Member{ID: "maria", HomeTenantID: "t", Name: "Maria Lopez"}, Member{ID: "sam", HomeTenantID: "t", Name: "Sam Reed"})
	return m
}

func TestIssue3808_WorkerCardSaysWhatIsMissingAndOffersFixPunch(t *testing.T) {
	m := issue3808Model()
	got := renderNode(t, message(m, handlers{}, issue3808Message(t, chat.ClockCardSelf), false))
	for _, want := range []string{"You have a missing punch for Tuesday", "Fix punch", `href="/workspace/app/time/missing-punch"`, "chat-clock-card"} {
		if !strings.Contains(got, want) {
			t.Errorf("worker card lacks %q: %s", want, got)
		}
	}
	for _, bad := range []string{"clock-card:", "sess-1", "maria", "message-actions"} {
		if strings.Contains(got, bad) {
			t.Errorf("worker card leaks %q: %s", bad, got)
		}
	}
}

// The supervisor is told whose punch it is and nothing the worker's own page
// would act on: no button, no session reference.
func TestIssue3808_SupervisorCardNamesTheWorkerWithoutAnAction_Security(t *testing.T) {
	m := issue3808Model()
	m.CurrentUser = "sam"
	got := renderNode(t, message(m, handlers{}, issue3808Message(t, chat.ClockCardReview), false))
	if !strings.Contains(got, "Maria Lopez") || !strings.Contains(got, "needs review") {
		t.Errorf("supervisor card: %s", got)
	}
	for _, bad := range []string{"Fix punch", "<a ", "href=", "sess-1", "clock-card:"} {
		if strings.Contains(got, bad) {
			t.Errorf("supervisor card carries %q: %s", bad, got)
		}
	}
	// A name the reader does not hold is not guessed.
	m.Members = nil
	if got := renderNode(t, message(m, handlers{}, issue3808Message(t, chat.ClockCardReview), false)); !strings.Contains(got, "A teammate") || strings.Contains(got, "maria") {
		t.Errorf("unnamed worker: %s", got)
	}
}

func TestIssue3808_CardIsLocalizedAndNeverPrintsACopyKey(t *testing.T) {
	m := issue3808Model()
	for locale, want := range map[string][]string{
		"de-DE": {"Dienstag", "Stempelung korrigieren"},
		"ar":    {"الثلاثاء", "تصحيح البصمة"},
	} {
		m.Locale = locale
		got := renderNode(t, message(m, handlers{}, issue3808Message(t, chat.ClockCardSelf), false))
		for _, w := range want {
			if !strings.Contains(got, w) {
				t.Errorf("%s card lacks %q: %s", locale, w, got)
			}
		}
		if regexp.MustCompile(`chat\.issue3808`).MatchString(got) {
			t.Errorf("%s card prints a copy key: %s", locale, got)
		}
	}
}

// Every surface that shows a message as text (Saved, previews, notices) reads
// the card as its sentence, and the card never groups with its neighbours.
func TestIssue3808_CardReadsAsASentenceElsewhereAndNeverGroups(t *testing.T) {
	m := issue3808Model()
	msg := issue3808Message(t, chat.ClockCardSelf)
	text, ok := chatux021LineText(m, msg)
	if !ok || text != "You have a missing punch for Tuesday." {
		t.Errorf("line text = %q, %v", text, ok)
	}
	if !chatux021IsLine(msg) {
		t.Error("a card groups with the messages around it")
	}
}
