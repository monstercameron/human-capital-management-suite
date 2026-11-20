package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func renderGateNotice(t *testing.T, locale, outcome string) string {
	t.Helper()
	n := chat.ModerationNotice{TenantID: "t", ID: "gate-review:" + outcome + ":s1", ConversationID: "room-payroll", Outcome: outcome, At: time.Date(2026, 10, 2, 8, 0, 0, 0, time.UTC)}
	out, err := ui.RenderToString(ModerationAuthorNotice(locale, n))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestIssue3817_GateReviewNotice: the notice tells a reviewer a request is
// waiting, in their language, and links to the join form's review queue; it
// names no applicant and no answer.
func TestIssue3817_GateReviewNotice(t *testing.T) {
	want := map[string]map[string]string{
		"en-US": {chat.ModerationOutcomeGateReview: "A request to join a channel is waiting for your decision", chat.ModerationOutcomeGateOverdue: "has been waiting longer than planned", chat.ModerationOutcomeGateEscalated: "needs a decision from an administrator"},
		"de-DE": {chat.ModerationOutcomeGateReview: "wartet auf Ihre Entscheidung", chat.ModerationOutcomeGateOverdue: "wartet länger als geplant", chat.ModerationOutcomeGateEscalated: "braucht die Entscheidung eines Administrators"},
		"ar":    {chat.ModerationOutcomeGateReview: "بانتظار قرارك", chat.ModerationOutcomeGateOverdue: "ينتظر منذ مدة أطول من المخطط", chat.ModerationOutcomeGateEscalated: "قرار من مسؤول"},
	}
	for locale, byOutcome := range want {
		for outcome, text := range byOutcome {
			html := renderGateNotice(t, locale, outcome)
			if !strings.Contains(html, text) {
				t.Errorf("%s %s: missing %q in %s", locale, outcome, text, html)
			}
			if !strings.Contains(html, `data-gate-open="room-payroll"`) || !strings.Contains(html, `href="#gate=room-payroll"`) {
				t.Errorf("%s %s: no link to the review queue: %s", locale, outcome, html)
			}
			if !strings.Contains(html, `role="status"`) || strings.Contains(html, "filter") {
				t.Errorf("%s %s: wrong notice shape: %s", locale, outcome, html)
			}
		}
	}
}
