package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	xhtml "golang.org/x/net/html"
)

func issue3818Row(t *testing.T, m Model, id string) (string, *xhtml.Node) {
	t.Helper()
	markup := chatremoveMarkup(t, browseDialog(m, handlers{}))
	rows := chatPolishNodes(t, markup, func(n *xhtml.Node) bool {
		return n.Data == "li" && chatPolishAttr(n, "data-conversation-id") == id
	})
	if len(rows) != 1 {
		t.Fatalf("no row %s: %s", id, markup)
	}
	return markup, rows[0]
}

// TestIssue3818_BrowseShowsPendingRequest: after an applicant submits, the
// Browse row says the request was sent and who it waits for, no longer invites
// them to answer, and its button opens the form (where the request is
// withdrawn). A declined request shows its reason on the row.
func TestIssue3818_BrowseShowsPendingRequest(t *testing.T) {
	want := map[string]struct{ waiting, managers, button, declined, answerAgain string }{
		"en-US": {"Request sent, waiting for Mia Manager", "Request sent, waiting for the channel’s managers", "View or withdraw request", "Not admitted: Sales has its own channel", "Answer again"},
		"de-DE": {"Anfrage gesendet, wartet auf Mia Manager", "Anfrage gesendet, wartet auf die Verwalter des Kanals", "Anfrage ansehen oder zurückziehen", "Nicht zugelassen: Sales has its own channel", "Erneut antworten"},
		"ar":    {"تم إرسال الطلب، بانتظار Mia Manager", "تم إرسال الطلب، بانتظار مديري القناة", "عرض الطلب أو سحبه", "لم يتم قبولك: Sales has its own channel", "أجب مرة أخرى"},
	}
	for locale, w := range want {
		m := chatgateBrowseModel(locale)
		m.GateJoins["gated"] = GateJoin{Questions: 3, Mode: "review", Request: "review", Reviewers: []string{"Mia Manager"}}
		m.GateJoins["one"] = GateJoin{Questions: 1, Mode: "review", Request: "review"}
		m.GateJoins["open"] = GateJoin{Questions: 1, Mode: "review", Request: "declined", RequestReason: "Sales has its own channel"}
		for _, c := range []struct{ id, text, button string }{{"gated", w.waiting, w.button}, {"one", w.managers, w.button}, {"open", w.declined, w.answerAgain}} {
			markup, row := issue3818Row(t, m, c.id)
			text := issue3818Text(row)
			if !strings.Contains(text, c.text) || strings.Contains(text, "to join") || strings.Contains(text, "reads your answers") {
				t.Errorf("%s %s: row text %q, want %q", locale, c.id, text, c.text)
			}
			buttons := chatPolishNodes(t, markup, func(n *xhtml.Node) bool {
				return n.Data == "button" && chatPolishAttr(n, "data-id") == c.id && chatPolishAttr(n, "data-gate-open") == c.id
			})
			if len(buttons) != 1 || !strings.Contains(issue3818Text(buttons[0]), c.button) {
				t.Errorf("%s %s: no button %q opening the form in %s", locale, c.id, c.button, markup)
			}
			status := chatPolishNodes(t, markup, func(n *xhtml.Node) bool {
				return strings.Contains(chatPolishAttr(n, "class"), "chatgate-request") && issue3818Text(n) != ""
			})
			if len(status) == 0 || chatPolishAttr(status[0], "role") != "status" {
				t.Errorf("%s %s: the request line is not announced as a status", locale, c.id)
			}
		}
	}
}

// A member and a person with no request see what they saw before.
func TestIssue3818_BrowseWithoutRequestIsUnchanged(t *testing.T) {
	m := chatgateBrowseModel("en-US")
	_, row := issue3818Row(t, m, "gated")
	if text := issue3818Text(row); !strings.Contains(text, "Answer 3 questions to join") || strings.Contains(text, "Request sent") {
		t.Fatalf("row: %q", text)
	}
}

// TestIssue3818_DecisionNotice: the applicant's notice says they were let in
// (with a link to the channel) or not admitted (with the reason and a link to
// their request), in each language.
func TestIssue3818_DecisionNotice(t *testing.T) {
	want := map[string]map[string]string{
		"en-US": {chat.ModerationOutcomeGateAdmitted: "You were let in", chat.ModerationOutcomeGateDeclined: "You were not admitted"},
		"de-DE": {chat.ModerationOutcomeGateAdmitted: "Sie wurden eingelassen", chat.ModerationOutcomeGateDeclined: "Sie wurden nicht zugelassen"},
		"ar":    {chat.ModerationOutcomeGateAdmitted: "تم قبولك", chat.ModerationOutcomeGateDeclined: "لم يتم قبولك"},
	}
	for locale, byOutcome := range want {
		for outcome, text := range byOutcome {
			n := chat.ModerationNotice{TenantID: "t", ID: "gate-decision:s:2", ConversationID: "sales", Outcome: outcome, Reason: "Sales has its own channel", At: time.Date(2026, 10, 2, 8, 0, 0, 0, time.UTC)}
			html := chatremoveMarkup(t, ModerationAuthorNotice(locale, n))
			if !strings.Contains(html, text) || !strings.Contains(html, `role="status"`) {
				t.Errorf("%s %s: %s", locale, outcome, html)
			}
			if outcome == chat.ModerationOutcomeGateAdmitted {
				if !strings.Contains(html, `href="`+ChannelReferenceURL("sales")+`"`) || strings.Contains(html, "Sales has its own channel") {
					t.Errorf("%s admitted: wrong link or reason: %s", locale, html)
				}
			} else if !strings.Contains(html, "Sales has its own channel") || !strings.Contains(html, `data-gate-open="sales"`) || !strings.Contains(html, `dir="auto"`) {
				t.Errorf("%s declined: reason or request link missing: %s", locale, html)
			}
		}
	}
}

func issue3818Text(n *xhtml.Node) string {
	var b strings.Builder
	var walk func(*xhtml.Node)
	walk = func(x *xhtml.Node) {
		if x.Type == xhtml.TextNode {
			b.WriteString(x.Data)
			b.WriteString(" ")
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(b.String()), " ")
}
