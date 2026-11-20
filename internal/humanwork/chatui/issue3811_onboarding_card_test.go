package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func issue3811Message(t *testing.T) Message {
	t.Helper()
	body, err := chat.OnboardingCardBody(chat.OnboardingCard{Subject: "maria", Worker: "worker-77", Employment: "emp-9"})
	if err != nil {
		t.Fatal(err)
	}
	return Message{ID: "welcome", AuthorID: "system", Author: "People", Body: body, TimeLabel: "9:41"}
}

func issue3811Render(t *testing.T, m Model) string {
	t.Helper()
	return renderNode(t, message(m, handlers{}, issue3811Message(t), false))
}

func issue3811Active() OnboardingCardView {
	return OnboardingCardView{Loaded: true, State: "active", StartDate: "2026-11-17", Items: []OnboardingItemView{
		{Kind: "IDENTITY", Owner: "People team", Status: "confirmed", ConfirmedOn: "2026-11-10", Required: true},
		{Kind: "ACCESS", Owner: "IT desk", Status: "pending", Required: true},
		{Kind: "MEDICAL", Owner: "Occupational health", Status: "unverified"},
		{Kind: "TRAINING", Owner: "", Status: "attention"},
	}}
}

func TestIssue3811_CardShowsTheOwnersChecklistWithStatusAndContact(t *testing.T) {
	m := issue3808Model()
	m.Onboarding.Views = map[string]OnboardingCardView{"welcome": issue3811Active()}
	got := issue3811Render(t, m)
	for _, want := range []string{
		"Welcome to your first days", "Your plan starts", "Identity", "Confirmed", "Confirmed on",
		"Access", "Still to do", "Ask: IT desk", "Health", "Not verified yet", "Ask: Occupational health",
		"Needs attention", "No contact is named yet.", "Open my onboarding tasks", `href="/workspace/app/onboarding/tasks"`,
		"People records, not from this chat", "chat-onboarding-card", `aria-label="Your onboarding checklist"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("card lacks %q: %s", want, got)
		}
	}
	// Nothing on the card says the person is ready, and no internal id or
	// marker reaches the page.
	for _, bad := range []string{"onboarding-card:", "worker-77", "emp-9", "READY", "You are ready", "message-actions"} {
		if strings.Contains(got, bad) {
			t.Errorf("card leaks %q: %s", bad, got)
		}
	}
}

func TestIssue3811_CardStatesNeverClaimReadiness(t *testing.T) {
	m := issue3808Model()
	for name, tc := range map[string]struct {
		view OnboardingCardView
		want string
	}{
		"loading":     {OnboardingCardView{}, "Checking your checklist..."},
		"unreachable": {OnboardingCardView{Loaded: true, State: "unavailable"}, "cannot be checked right now"},
		"ended":       {OnboardingCardView{Loaded: true, State: "ended"}, "no longer applies"},
		"cancelled":   {OnboardingCardView{Loaded: true, State: "cancelled", StartDate: "2026-11-17"}, "was cancelled"},
		"closed":      {OnboardingCardView{Loaded: true, State: "closed"}, "onboarding is complete"},
		"not for you": {OnboardingCardView{Loaded: true}, "private to the person"},
		"empty":       {OnboardingCardView{Loaded: true, State: "active"}, "nothing on your checklist yet"},
	} {
		m.Onboarding.Views = map[string]OnboardingCardView{"welcome": tc.view}
		got := issue3811Render(t, m)
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: lacks %q: %s", name, tc.want, got)
		}
		for _, bad := range []string{"chat-onboarding-list", "Confirmed on"} {
			if tc.view.State != "active" && strings.Contains(got, bad) {
				t.Errorf("%s: shows items it does not have (%q): %s", name, bad, got)
			}
		}
	}
	// An unrecognised status from a newer server reads as not verified, never done.
	m.Onboarding.Views = map[string]OnboardingCardView{"welcome": {Loaded: true, State: "active", Items: []OnboardingItemView{{Kind: "ACCESS", Status: "done!"}}}}
	if got := issue3811Render(t, m); !strings.Contains(got, "Not verified yet") || strings.Contains(got, "Confirmed") {
		t.Errorf("unknown status: %s", got)
	}
}

func TestIssue3811_CardCopyIsCompleteInEveryLanguage(t *testing.T) {
	for _, locale := range []string{"de-DE", "ar"} {
		for key, en := range issue3811Copy["en-US"] {
			if got := issue3811Copy[locale][key]; got == "" || got == en {
				t.Errorf("%s: %q is missing or untranslated (%q)", locale, key, got)
			}
		}
	}
	m := issue3808Model()
	m.Locale = "ar"
	m.Onboarding.Views = map[string]OnboardingCardView{"welcome": issue3811Active()}
	got := issue3811Render(t, m)
	if !strings.Contains(got, "مرحبًا بك في أيامك الأولى") || strings.Contains(got, "Welcome to your first days") {
		t.Errorf("arabic card: %s", got)
	}
}

// The welcome is a system row: it never groups with the messages around it and
// the page treats it as a line, not a message with reply and react actions.
func TestIssue3811_CardIsASystemRow(t *testing.T) {
	msg := issue3811Message(t)
	if !chatux021IsLine(msg) || !IsOnboardingCardBody(msg.Body) {
		t.Fatal("the welcome card is not treated as a system line")
	}
	if IsOnboardingCardBody("hello") {
		t.Error("plain text is not a card")
	}
	if !strings.Contains(Issue3811Styles, "min-block-size:44px") || !strings.Contains(ChatLane3Styles, "chat-onboarding-card") {
		t.Error("the card's styles are missing the touch target or are not shipped")
	}
}
