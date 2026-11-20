package chatui

import (
	"strings"
	"testing"
)

func issue3779Held(locale string, count int) Model {
	m := issue3812Model(locale, "hr")
	m.Hold = HoldState{ConversationID: "thread", Loaded: true, Held: true, Count: count}
	return m
}

func TestIssue3779_DetailsShowAnActiveHoldBadge(t *testing.T) {
	panel := renderNode(t, chatux005Details(issue3779Held("en-US", 1), handlers{}))
	for _, want := range []string{"Legal hold", "On legal hold", `data-hold-badge="true"`, `role="note"`, "Retention never removes anything in this conversation", "Only workspace administrators see this"} {
		if !strings.Contains(panel, want) {
			t.Errorf("details lack %q", want)
		}
	}
	if strings.Contains(panel, "holds are in force") {
		t.Errorf("a single hold must not read as several: %s", panel)
	}
	if many := renderNode(t, issue3779Section(issue3779Held("en-US", 3))); !strings.Contains(many, "3 holds are in force on this conversation.") {
		t.Errorf("several holds are counted: %s", many)
	}
}

func TestIssue3779_DetailsShowNothingWithoutAnAnswerForThisConversation_Security(t *testing.T) {
	for name, mutate := range map[string]func(*Model){
		"not loaded":         func(m *Model) { m.Hold.Loaded = false },
		"another room":       func(m *Model) { m.Hold.ConversationID = "elsewhere" },
		"not held":           func(m *Model) { m.Hold.Held = false },
		"nothing was asked":  func(m *Model) { m.Hold = HoldState{} },
		"stale for the room": func(m *Model) { m.SelectedID = "other-thread" },
	} {
		m := issue3779Held("en-US", 1)
		mutate(&m)
		if got := issue3779Section(m); got != nil {
			t.Errorf("%s: the details show a hold: %s", name, renderNode(t, got))
		}
	}
}

func TestIssue3779_HoldBadgeLocalised(t *testing.T) {
	for locale, want := range map[string]string{"de-DE": "Unter Aufbewahrungspflicht", "ar": "تحت حجز قانوني"} {
		if got := renderNode(t, issue3779Section(issue3779Held(locale, 1))); !strings.Contains(got, want) {
			t.Errorf("%s lacks %q: %s", locale, want, got)
		}
	}
	for locale, table := range issue3779Copy {
		for _, key := range []string{keyIssue3779Title, keyIssue3779Badge, keyIssue3779Help, keyIssue3779Many, keyIssue3779Where} {
			if strings.TrimSpace(table[key]) == "" {
				t.Errorf("%s has no text for %s", locale, key)
			}
		}
	}
}
