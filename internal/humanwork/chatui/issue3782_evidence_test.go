package chatui

import (
	"strings"
	"testing"
	"time"
)

func issue3782Model(locale, user string) Model {
	m := issue3812Bound(issue3812Model(locale, user), true, false)
	m.Messages = []Message{
		{ID: "m1", AuthorID: "mgr", Author: "Max Manager", Body: "I asked for the leave on Monday.", TimeLabel: "9:41 AM", SentAt: time.Date(2026, 11, 19, 9, 41, 0, 0, time.UTC)},
		{ID: "m2", AuthorID: "hr", Author: "Hana Roth", Body: "Noted, thank you.", TimeLabel: "9:44 AM", SentAt: time.Date(2026, 11, 19, 9, 44, 0, 0, time.UTC)},
	}
	m.Evidence = EvidenceState{ConversationID: "thread", Loaded: true, Bound: true, CaseRef: "ER-2041", CanAttach: user == "hr"}
	return m
}

func issue3782WithEvidence(m Model) Model {
	at := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	m.Evidence.Excerpts = []EvidenceExcerpt{{ID: "e1", CaseRevision: "rev-3", Note: "Shows when it was asked", AttachedBy: "hr", AttachedAt: at, Items: []EvidenceItem{
		{ID: "i1", Status: "current", AuthorID: "mgr", AuthoredAt: at.Add(-time.Hour), Body: "I asked for the leave on Monday.", Fingerprint: "a1b2c3d4e5f6"},
		{ID: "i2", Status: "edited", AuthorID: "hr", AuthoredAt: at.Add(-50 * time.Minute), Body: "Frozen words", Fingerprint: "ffeeddccbbaa"},
		{ID: "i3", Status: "gone"},
	}}}
	return m
}

func TestIssue3782_OwnerSeesAReviewableAttachForm(t *testing.T) {
	m := issue3782Model("en-US", "hr")
	got := renderNode(t, issue3782Section(m))
	for _, want := range []string{
		`data-evidence-section="true"`, "Case evidence", "ER-2041",
		// The selection, with the content to be copied.
		"I asked for the leave on Monday.", "Noted, thank you.", `name="message"`, `value="m1"`, `value="m2"`,
		// Destination and audience, before submission.
		"Case thread", "3 in all", "Proposal revision", `name="case_revision"`, `name="note"`, `name="reviewed"`,
		"Attach selected messages", `data-evidence-form="thread"`,
		// It never claims a decision.
		"do not record an approval",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("section lacks %q\n%s", want, got)
		}
	}
	for _, bad := range []string{"Approved by", "Outcome:", "Policy version"} {
		if strings.Contains(got, bad) {
			t.Errorf("section claims %q", bad)
		}
	}
}

func TestIssue3782_ParticipantSeesEvidenceButNoForm(t *testing.T) {
	m := issue3782WithEvidence(issue3782Model("en-US", "mgr"))
	got := renderNode(t, issue3782Section(m))
	if strings.Contains(got, "data-evidence-form") || strings.Contains(got, `name="message"`) {
		t.Fatalf("a participant who cannot attach is offered the form:\n%s", got)
	}
	for _, want := range []string{"Hana Roth", "Shows when it was asked", "rev-3", "I asked for the leave on Monday.", "Frozen words", "a1b2c3d4e5f6", "Edited since it was attached"} {
		if !strings.Contains(got, want) {
			t.Errorf("evidence lacks %q\n%s", want, got)
		}
	}
}

func TestIssue3782_GoneSourceWithholdsTheText(t *testing.T) {
	m := issue3782WithEvidence(issue3782Model("en-US", "mgr"))
	got := renderNode(t, issue3782Section(m))
	if !strings.Contains(got, "deleted or is no longer open to you") || !strings.Contains(got, `data-evidence-status="gone"`) {
		t.Fatalf("a gone source is not marked:\n%s", got)
	}
	// Status is text, not colour alone, on every state.
	for _, status := range []string{"current", "edited", "gone"} {
		if !strings.Contains(got, `data-evidence-status="`+status+`"`) {
			t.Errorf("no %s marker", status)
		}
	}
}

func TestIssue3782_ShowsNothingWithoutAnAnswerForThisConversation(t *testing.T) {
	for name, mutate := range map[string]func(*Model){
		"not loaded":    func(m *Model) { m.Evidence.Loaded = false },
		"another room":  func(m *Model) { m.Evidence.ConversationID = "elsewhere" },
		"unbound":       func(m *Model) { m.Evidence.Bound = false },
		"nothing asked": func(m *Model) { m.Evidence = EvidenceState{} },
		"stale":         func(m *Model) { m.SelectedID = "other-thread" },
		"nothing to do": func(m *Model) { m.Evidence.CanAttach = false },
	} {
		m := issue3782Model("en-US", "hr")
		mutate(&m)
		if got := issue3782Section(m); got != nil {
			t.Errorf("%s: section shown: %s", name, renderNode(t, got))
		}
	}
}

func TestIssue3782_NoticesAreAnnouncedAndBusyIsDisabled(t *testing.T) {
	m := issue3782Model("en-US", "hr")
	m.Evidence.Notice, m.Evidence.Busy = "changed", true
	got := renderNode(t, issue3782Section(m))
	for _, want := range []string{`role="alert"`, "bound to a different case", `aria-busy="true"`, "Attaching..."} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks %q\n%s", want, got)
		}
	}
	m.Evidence.Notice, m.Evidence.Busy = "done", false
	if got := renderNode(t, issue3782Section(m)); !strings.Contains(got, `role="status"`) || !strings.Contains(got, "attached") {
		t.Errorf("success is not announced politely:\n%s", got)
	}
}

func TestIssue3782_NoMessagesToChoose(t *testing.T) {
	m := issue3782Model("en-US", "hr")
	m.Messages = nil
	got := renderNode(t, issue3782Section(m))
	if strings.Contains(got, `name="message"`) || !strings.Contains(got, "No messages are loaded") {
		t.Fatalf("an empty selection must say so and offer nothing to submit:\n%s", got)
	}
}

func TestIssue3782_CopyIsCompleteInEveryLanguage(t *testing.T) {
	en := issue3782Copy["en-US"]
	if len(en) < 20 {
		t.Fatalf("english copy has %d entries", len(en))
	}
	for _, locale := range []string{"de-DE", "ar"} {
		for key, english := range en {
			text := issue3782Copy[locale][key]
			if strings.TrimSpace(text) == "" {
				t.Errorf("%s lacks %s", locale, key)
			}
			if text == english {
				t.Errorf("%s %s is not translated", locale, key)
			}
			if strings.Count(text, "{") != strings.Count(english, "{") {
				t.Errorf("%s %s keeps different placeholders: %q", locale, key, text)
			}
		}
		if extra := len(issue3782Copy[locale]) - len(en); extra != 0 {
			t.Errorf("%s has %d entries differing from english", locale, extra)
		}
	}
	for _, locale := range []string{"de-DE", "ar"} {
		m := issue3782WithEvidence(issue3782Model(locale, "hr"))
		got := renderNode(t, issue3782Section(m))
		if strings.Contains(got, "Case evidence") || strings.Contains(got, "Attach selected messages") {
			t.Errorf("%s renders english", locale)
		}
	}
}

func TestIssue3782_StylesAreRightToLeftAndPhoneSafe(t *testing.T) {
	for _, want := range []string{"min-block-size:44px", "overflow-wrap:anywhere", "prefers-reduced-motion"} {
		if !strings.Contains(Issue3782Styles, want) {
			t.Errorf("styles lack %q", want)
		}
	}
	for _, physical := range []string{"margin-left", "margin-right", "padding-left", "padding-right", "text-align:left", "text-align:right", "border-left", "border-right", "float:left", "float:right"} {
		if strings.Contains(Issue3782Styles, physical) {
			t.Errorf("styles use the physical property %q", physical)
		}
	}
	if !strings.Contains(ChatLane3Styles, Issue3782Styles) {
		t.Error("the evidence styles are not part of the chat styles")
	}
}

func TestIssue3782_DetailsPanelContainsTheSection(t *testing.T) {
	m := issue3782WithEvidence(issue3782Model("en-US", "hr"))
	if got := renderNode(t, issue3782Section(m)); !strings.Contains(got, "Case evidence") {
		t.Fatal("section does not render")
	}
	src := renderNode(t, chatux005Details(m, handlers{}))
	if !strings.Contains(src, `data-evidence-section="true"`) {
		t.Errorf("the details panel does not include the evidence section")
	}
}
