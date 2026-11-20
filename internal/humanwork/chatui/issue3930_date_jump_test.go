package chatui

import (
	"strings"
	"testing"
	"time"
)

func issue3930Model() Model {
	return Model{
		Locale:     "en-US",
		SelectedID: "design",
		Conversations: []Conversation{
			{ID: "design", Name: "design", Kind: PublicChannel},
		},
		Callbacks: Callbacks{JumpToDate: func(string) {}, JumpBackFromDate: func() {}},
	}
}

func TestIssue3930_HeaderOffersJumpToDate(t *testing.T) {
	m := issue3930Model()
	markup := renderNode(t, issue3930Button(m, handlers{}))
	for _, want := range []string{`data-action="date-jump-toggle"`, `aria-label="Jump to date"`, `aria-expanded="false"`, `aria-controls="chat-date-jump-bar"`, `icon-calendar`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("header button lacks %q:\n%s", want, markup)
		}
	}
	m.Callbacks.JumpToDate = nil
	if got := renderNode(t, issue3930Button(m, handlers{})); !strings.Contains(got, "disabled") {
		t.Fatalf("a client that cannot jump must not offer a live button:\n%s", got)
	}
	open := renderNode(t, issue3930Button(issue3930Model(), handlers{local: localUI{dateJumpOpen: true}}))
	if !strings.Contains(open, `aria-expanded="true"`) {
		t.Fatalf("open state not announced:\n%s", open)
	}
}

func TestIssue3930_BarIsAbsentUntilOpenedOrThereIsAWayBack(t *testing.T) {
	m := issue3930Model()
	if issue3930Bar(m, handlers{}) != nil {
		t.Fatal("the bar is drawn before anyone asked for it")
	}
	m.DateJump = DateJumpView{ConversationID: "other", Status: DateJumpExact, CanGoBack: true}
	if issue3930Bar(m, handlers{}) != nil {
		t.Fatal("another conversation's jump state leaked into this one")
	}
}

func TestIssue3930_OpenBarHasLabelledDateFieldAndLiveStatus(t *testing.T) {
	m := issue3930Model()
	m.DateJump = DateJumpView{ConversationID: "design", Status: DateJumpLater, Date: "2026-03-05", Landed: time.Date(2026, 3, 9, 9, 0, 0, 0, time.Local)}
	markup := renderNode(t, issue3930Bar(m, handlers{local: localUI{dateJumpOpen: true}}))
	for _, want := range []string{`id="chat-date-jump-bar"`, `for="chat-date-jump"`, `type="date"`, `id="chat-date-jump"`, `max="`, `data-chat-value="2026-03-05"`, `role="status"`, `aria-live="polite"`, `data-action="date-jump-go"`, `data-action="date-jump-close"`, "No messages on"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("open bar lacks %q:\n%s", want, markup)
		}
	}
}

func TestIssue3930_BackToWhereYouWereStaysAfterTheBarCloses(t *testing.T) {
	m := issue3930Model()
	m.DateJump = DateJumpView{ConversationID: "design", Status: DateJumpExact, Date: "2026-03-05", Landed: time.Date(2026, 3, 5, 9, 0, 0, 0, time.Local), CanGoBack: true}
	markup := renderNode(t, issue3930Bar(m, handlers{}))
	if !strings.Contains(markup, `data-action="date-jump-back"`) || !strings.Contains(markup, "Back to where you were") {
		t.Fatalf("no way back after a jump:\n%s", markup)
	}
	if strings.Contains(markup, `type="date"`) {
		t.Fatalf("a closed bar still shows the date field:\n%s", markup)
	}
	m.Callbacks.JumpBackFromDate = nil
	if got := renderNode(t, issue3930Bar(m, handlers{})); !strings.Contains(got, "disabled") {
		t.Fatalf("back button live without a callback:\n%s", got)
	}
}

func TestIssue3930_StatusExplainsEachOutcomeInEveryLanguage(t *testing.T) {
	landed := time.Date(2026, 3, 9, 9, 0, 0, 0, time.Local)
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		seen := map[string]string{}
		for _, status := range []string{DateJumpLoading, DateJumpExact, DateJumpLater, DateJumpEarliest, DateJumpNewest, DateJumpEmpty, DateJumpError, DateJumpInvalid} {
			m := issue3930Model()
			m.Locale = locale
			view := DateJumpView{ConversationID: "design", Status: status, Date: "2026-03-05", Landed: landed}
			text := issue3930Status(m, view)
			if strings.TrimSpace(text) == "" || strings.Contains(text, "{") || strings.Contains(text, "chat.issue3930") {
				t.Fatalf("%s %s: status %q is empty or unfilled", locale, status, text)
			}
			if prior, dup := seen[text]; dup {
				t.Fatalf("%s: %s and %s say the same thing %q", locale, status, prior, text)
			}
			seen[text] = status
		}
	}
	en := issue3930Model()
	if got := issue3930Status(en, DateJumpView{Status: DateJumpEarliest, Landed: landed}); !strings.Contains(got, "oldest") || strings.Contains(strings.ToLower(got), "no discussion") {
		t.Fatalf("history that starts later must say what is available, not that nothing happened: %q", got)
	}
	de := issue3930Model()
	de.Locale = "de-DE"
	if got := issue3930Button(de, handlers{}); !strings.Contains(renderNode(t, got), "Zu Datum springen") {
		t.Fatal("de-DE button label missing")
	}
}

func TestIssue3930_FieldRefusesFutureDays(t *testing.T) {
	if got, want := issue3930MaxDate(time.Date(2026, 3, 5, 23, 59, 0, 0, time.Local)), "2026-03-05"; got != want {
		t.Fatalf("max = %q, want %q", got, want)
	}
}
