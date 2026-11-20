package chatui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func issue3728Message(t *testing.T, outcome string, open bool) Message {
	t.Helper()
	body, err := chat.JourneyStageBody(chat.JourneyStageLine{Outcome: outcome, Decider: "loretta"})
	if err != nil {
		t.Fatal(err)
	}
	msg := Message{ID: "stage", AuthorID: "loretta", Author: "loretta", Body: body, TimeLabel: "9:41"}
	if open {
		msg.WorkReferences = []ChatReference{{Kind: ChatReferenceJourney, TenantID: "t", ID: "j-42", Display: "Promotion request"}}
	}
	return msg
}

func TestIssue3728_DecisionLineNamesTheDeciderAndOffersTheRequest(t *testing.T) {
	m := chatux021Model()
	for outcome, want := range map[string]string{
		chat.JourneyStageApproved: "Loretta Haynes approved a workflow request.",
		chat.JourneyStageRejected: "Loretta Haynes rejected a workflow request.",
		chat.JourneyStageBlocked:  "A workflow request approved by Loretta Haynes is now blocked.",
	} {
		got := renderNode(t, message(m, handlers{}, issue3728Message(t, outcome, true), false))
		for _, w := range []string{want, "chatux021-system-line", `href="/workspace/app/journeys?journey=j-42"`, "View request", "9:41"} {
			if !strings.Contains(got, w) {
				t.Errorf("%s line lacks %q: %s", outcome, w, got)
			}
		}
		for _, bad := range []string{"journey-stage", "|", "message-actions", "avatar"} {
			if strings.Contains(got, bad) {
				t.Errorf("%s line leaks %q: %s", outcome, bad, got)
			}
		}
	}
}

// A reader who may not open the request gets no outcome, no decider and no
// link: the server dropped the reference for them, and the line says only that
// something shared here changed.
func TestIssue3728_ReaderWithoutAccessLearnsNoOutcomeNoNameNoLink_Security(t *testing.T) {
	m := chatux021Model()
	for _, outcome := range []string{chat.JourneyStageApproved, chat.JourneyStageRejected, chat.JourneyStageBlocked} {
		got := renderNode(t, message(m, handlers{}, issue3728Message(t, outcome, false), false))
		if !strings.Contains(got, "A workflow request shared here was updated.") {
			t.Errorf("%s: %s", outcome, got)
		}
		for _, bad := range []string{"Loretta", "approved", "rejected", "blocked", "<a ", "href=", "j-42", "journey-stage"} {
			if strings.Contains(got, bad) {
				t.Errorf("%s: restricted reader sees %q: %s", outcome, bad, got)
			}
		}
	}
}

func TestIssue3728_LineIsASystemLineEverySurfaceShowsAsText(t *testing.T) {
	m := chatux021Model()
	msg := issue3728Message(t, chat.JourneyStageApproved, true)
	if !chatux021IsLine(msg) {
		t.Fatal("a decision line must not group with the messages around it")
	}
	text, ok := chatux021LineText(m, msg)
	if !ok || text != "Loretta Haynes approved a workflow request." {
		t.Fatalf("text %q %v", text, ok)
	}
}

func TestIssue3728_UnknownDeciderIsNotGuessed(t *testing.T) {
	m := chatux021Model()
	m.Members = nil
	msg := issue3728Message(t, chat.JourneyStageRejected, true)
	got := renderNode(t, message(m, handlers{}, msg, false))
	if !strings.Contains(got, "Someone rejected a workflow request.") || strings.Contains(got, "loretta") {
		t.Fatalf("unnamed decider: %s", got)
	}
}

func TestIssue3728_LineIsLocalizedAndNeverPrintsACopyKey(t *testing.T) {
	m := chatux021Model()
	for locale, want := range map[string][]string{
		"de-DE": {"Loretta Haynes hat eine Workflow-Anfrage genehmigt.", "Anfrage ansehen"},
		"ar":    {"وافق Loretta Haynes على طلب سير عمل.", "عرض الطلب"},
	} {
		m.Locale = locale
		got := renderNode(t, message(m, handlers{}, issue3728Message(t, chat.JourneyStageApproved, true), false))
		for _, w := range want {
			if !strings.Contains(got, w) {
				t.Errorf("%s line lacks %q: %s", locale, w, got)
			}
		}
		if regexp.MustCompile(`chat\.issue3728`).MatchString(got) {
			t.Errorf("%s line prints a copy key: %s", locale, got)
		}
		// The restricted sentence exists in every language too.
		restricted := renderNode(t, message(m, handlers{}, issue3728Message(t, chat.JourneyStageApproved, false), false))
		if strings.Contains(restricted, "workflow request") || regexp.MustCompile(`chat\.issue3728`).MatchString(restricted) {
			t.Errorf("%s restricted line: %s", locale, restricted)
		}
	}
}

// The link is a 44px target that wraps at 320px and follows the reading
// direction, like the other system lines.
func TestIssue3728_LinkStyleIsATouchTargetWithLogicalProperties(t *testing.T) {
	for _, want := range []string{".chatux021-line-link", "min-block-size:44px"} {
		if !strings.Contains(Issue3728Styles, want) {
			t.Errorf("styles lack %q", want)
		}
	}
	for _, bad := range []string{"margin-left", "margin-right", "padding-left", "padding-right", "text-align:left", "text-align:right"} {
		if strings.Contains(Issue3728Styles, bad) {
			t.Errorf("styles use a physical property %q", bad)
		}
	}
	if !strings.Contains(ChatLane3Styles, Issue3728Styles) {
		t.Error("the styles are not part of the chat stylesheet")
	}
}
