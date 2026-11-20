package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
)

func issue3819View(locale string) GateView {
	v := chatgateViewFixture()
	v.Locale = locale
	v.Administrator = true
	v.Names = map[string]string{"worker": "Alex"}
	v.Submissions = []chatgate.Submission{{ID: "sub", Person: "worker", Status: "review", SubmittedAt: time.Date(2026, 1, 2, 3, 4, 0, 0, time.UTC)}}
	return v
}

// Issue 3819: an administrator who decides without a reason is told to give one,
// under the reason field, in words written for reviewers.
func TestIssue3819_ReasonProblem(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, action := range []string{"admit", "decline"} {
			got := GateReasonProblem(locale, action, "sub", "   ")
			msg := got["gate-reason-sub"]
			if len(got) != 1 || msg == "" || msg == GateText(locale, "next") || msg == GateText(locale, "invalid") {
				t.Fatalf("%s %s: %v", locale, action, got)
			}
			if GateReasonProblem(locale, action, "sub", "Welcome") != nil {
				t.Fatal("a given reason is a problem")
			}
		}
		if got := GateReasonProblem(locale, "bulk-admit", "", ""); got["gate-bulk-reason"] == "" {
			t.Fatalf("%s bulk: %v", locale, got)
		}
		if GateReasonProblem(locale, "submit", "sub", "") != nil {
			t.Fatal("only decisions need a reason")
		}
	}
	if got := GateReasonProblem("en-US", "admit", "sub", ""); got["gate-reason-sub"] != "Give a reason for this decision." {
		t.Fatal(got)
	}
}

func TestIssue3819_ReasonFieldIsMarkedInvalid(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		v := issue3819View(locale)
		clean := gateMarkup(t, v)
		if strings.Contains(clean, `id="gate-reason-sub" required aria-invalid="true"`) || strings.Contains(clean, `aria-invalid="true"`) {
			t.Fatalf("%s: marked invalid before any problem", locale)
		}
		v.FieldErrors = GateReasonProblem(locale, "admit", "sub", "")
		v.Notice = GateText(locale, "reasonNotice")
		s := gateMarkup(t, v)
		msg := v.FieldErrors["gate-reason-sub"]
		for _, want := range []string{`aria-invalid="true"`, `aria-describedby="gate-reason-sub-error"`, `id="gate-reason-sub-error"`, `role="alert"`, msg} {
			if !strings.Contains(s, want) {
				t.Fatalf("%s: missing %s in %s", locale, want, s)
			}
		}
		if strings.Contains(s, GateText(locale, "next")) {
			t.Fatalf("%s: applicant copy shown to the reviewer", locale)
		}
	}
}
