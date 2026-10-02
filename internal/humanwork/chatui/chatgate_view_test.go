package chatui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
)

func chatgateViewFixture() GateView {
	d := chatgate.Definition{Version: chatgate.Version{Major: 1}, Mode: "review", Purpose: "Discuss payroll", Fields: []chatgate.Field{{ID: "team", Kind: "single_choice", KindVersion: "1.0.0", Label: "Team", Purpose: "Welcome routing", DataClass: "INTERNAL", Required: true, RetentionDays: 30, Options: []string{"Payroll", "Finance"}, Visibility: chatgate.Visibility{Administrators: true}}}}
	return GateView{Locale: "en-US", Conversation: "room", Gate: chatgate.Gate{Versions: []chatgate.Definition{d}, Current: "1.0.0", State: "active"}}
}
func gateMarkup(t *testing.T, v GateView) string {
	t.Helper()
	s, e := ui.RenderToString(RenderGate(v))
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestTodo_CHATGATE_005(t *testing.T) {
	v := chatgateViewFixture()
	s := gateMarkup(t, v)
	for _, want := range []string{"Answer 1 question to join", "Why we ask: Welcome routing", "Who can see: Administrators", "Required", "Kept for 30 days", "Submit answers", "Save draft"} {
		if !strings.Contains(s, want) {
			t.Fatal(want)
		}
	}
	v.Submission = &chatgate.Submission{ID: "submission", Status: "review", SubmittedAt: time.Unix(1, 0).UTC()}
	v.Reviewers = []string{"Dana Review"}
	s = gateMarkup(t, v)
	if !strings.Contains(s, "Waiting for an administrator") || !strings.Contains(s, "Withdraw request") || !strings.Contains(s, "Dana Review") || !strings.Contains(s, "1970-01-01") {
		t.Fatal(s)
	}
	v.Submission.Status = "declined"
	v.Submission.Reason = "Team not eligible"
	if s = gateMarkup(t, v); !strings.Contains(s, "Team not eligible") || !strings.Contains(s, "submit again") {
		t.Fatal(s)
	}
}
func TestTodo_CHATGATE_005_Browser(t *testing.T) {
	v := chatgateViewFixture()
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		v.Locale = locale
		v.ConfirmWithdrawal = true
		s := gateMarkup(t, v)
		for _, key := range []string{"consequence", "confirm", "submit", "save"} {
			if !strings.Contains(s, GateText(locale, key)) {
				t.Fatal(locale, key)
			}
		}
		if locale == "ar" && !strings.Contains(s, `dir="rtl"`) {
			t.Fatal("missing RTL")
		}
	}
	v.Gate.Versions[0].Fields[0].Kind = "unknown"
	if s := gateMarkup(t, v); !strings.Contains(s, GateText(v.Locale, "unknown")) {
		t.Fatal("unknown field silently dropped")
	}
}
func TestTodo_CHATGATE_005_Accessibility(t *testing.T) {
	s := gateMarkup(t, chatgateViewFixture())
	for _, want := range []string{`for="gate-field-team"`, `aria-describedby="gate-field-team-hint gate-field-team-error"`, `id="gate-field-team-error"`, `role="alert"`, `type="button"`} {
		if !strings.Contains(s, want) {
			t.Fatal(want)
		}
	}
	for _, css := range []string{"min-block-size:44px", "focus-visible", "prefers-reduced-motion", "max-width:400px", "overflow-wrap:anywhere"} {
		if !strings.Contains(ChatgateStyles, css) {
			t.Fatal(css)
		}
	}
}
func TestTodo_CHATGATE_005_Security(t *testing.T) {
	v := chatgateViewFixture()
	v.Submissions = []chatgate.Submission{{Person: "hidden-member", Status: "review"}}
	v.VisibleAnswers = map[string]map[string]json.RawMessage{"hidden-member": {"team": json.RawMessage(`"hidden answer"`)}}
	v.Names = map[string]string{"hidden-member": "Secret Member"}
	s := gateMarkup(t, v)
	for _, secret := range []string{"hidden-member", "hidden answer", "Secret Member"} {
		if strings.Contains(s, secret) {
			t.Fatal("applicant leaked content", secret)
		}
	}
}
func TestTodo_CHATGATE_006(t *testing.T) {
	d := chatgateViewFixture().Gate.Versions[0]
	var e error
	d, e = EditGateQuestion(d, "question-add", 0)
	if e != nil || len(d.Fields) != 2 || d.Fields[1].RetentionDays != 30 || !d.Fields[1].Visibility.Administrators {
		t.Fatal(d, e)
	}
	id := d.Fields[1].ID
	d, e = EditGateQuestion(d, "question-up", 1)
	if e != nil || d.Fields[0].ID != id {
		t.Fatal(d, e)
	}
	d, e = EditGateQuestion(d, "question-remove", 0)
	if e != nil || len(d.Fields) != 1 || d.Fields[0].ID != "team" {
		t.Fatal(d, e)
	}
}
func TestTodo_CHATGATE_006_Browser(t *testing.T) {
	v := chatgateViewFixture()
	v.Administrator = true
	v.ExportAllowed = true
	v.Submissions = []chatgate.Submission{{ID: "sub", Person: "worker", Status: "review"}}
	v.Names = map[string]string{"worker": "Alex"}
	v.VisibleAnswers = map[string]map[string]json.RawMessage{"worker": {"team": json.RawMessage(`"Payroll"`)}}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		v.Locale = locale
		s := gateMarkup(t, v)
		for _, key := range []string{"add", "remove", "up", "down", "publish", "meaning", "queue", "admit", "decline", "export", "filter"} {
			if !strings.Contains(s, GateText(locale, key)) {
				t.Fatal(locale, key)
			}
		}
	}
}
func TestTodo_CHATGATE_006_Accessibility(t *testing.T) {
	v := chatgateViewFixture()
	v.Administrator = true
	s := gateMarkup(t, v)
	for _, id := range []string{"gate-purpose", "gate-mode", "gate-editor-0-label", "gate-editor-0-purpose", "gate-editor-0-days", "gate-version"} {
		if !strings.Contains(s, `for="`+id+`"`) {
			t.Fatal(id)
		}
	}
	for _, copy := range chatgateCopy {
		for key := range chatgateCopy["en-US"] {
			if strings.TrimSpace(copy[key]) == "" {
				t.Fatal("missing localized key", key)
			}
		}
	}
}
func TestTodo_CHATGATE_006_Security(t *testing.T) {
	v := chatgateViewFixture()
	v.Administrator = true
	v.ExportAllowed = false
	v.VisibleAnswers = map[string]map[string]json.RawMessage{"worker": {"team": json.RawMessage(`"Payroll"`)}}
	s := gateMarkup(t, v)
	if strings.Contains(s, `data-gate-action="export"`) {
		t.Fatal("export offered without permission")
	}
	v.Gate.Versions[0].Fields[0].Label = `<script>alert(1)</script>`
	s = gateMarkup(t, v)
	if strings.Contains(s, "<script>alert") {
		t.Fatal("unescaped question")
	}
}
func TestTodo_CHATGATE_006_AnswerCounts(t *testing.T) {
	v := chatgateViewFixture()
	v.VisibleAnswers = map[string]map[string]json.RawMessage{"one": {"team": json.RawMessage(`"Payroll"`)}, "two": {"team": json.RawMessage(`"Payroll"`)}}
	counts := GateAnswerCounts(v)
	if counts["team"]["Payroll"] != 2 || len(counts) != 1 {
		t.Fatal(counts)
	}
}
