package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func issue3793Model(locale string, available IntentRequestAvailable) Model {
	m := chatux021Model()
	m.Locale = locale
	m.Members = append(m.Members, Member{ID: "maria", HomeTenantID: "t", Name: "Maria Lopez"})
	m.IntentRequests = IntentRequestsState{
		ConversationID: m.SelectedID,
		Commands:       map[string]IntentRequestAvailable{"leave": available},
		Open:           func(string) {},
	}
	return m
}

func issue3793Open(m Model, phase string) Model {
	m.IntentRequests.View = IntentRequestView{Open: true, Command: "leave", ConversationID: m.SelectedID, Phase: phase,
		Values: map[string]string{"employee": "maria", "starts": "2026-12-01", "ends": "2026-12-05", "reason": "Family trip"}, Via: "HR Assistant"}
	return m
}

func issue3793Listed(m Model) bool {
	for _, c := range composerCommandsFor("chat-composer").menu(m, "") {
		if c.Name == "leave" {
			return true
		}
	}
	return false
}

func TestIssue3793_LeaveIsListedOnlyWhereTheServerSaidRequestsExist(t *testing.T) {
	if !issue3793Listed(issue3793Model("en-US", IntentRequestAvailable{Loaded: true, Available: true})) {
		t.Error("an available request must be in the command list")
	}
	if !issue3793Listed(issue3793Model("en-US", IntentRequestAvailable{Loaded: true, Reason: "paused"})) {
		t.Error("a paused request stays listed so choosing it explains why")
	}
	if issue3793Listed(issue3793Model("en-US", IntentRequestAvailable{Loaded: true, Reason: "not_installed"})) {
		t.Error("a conversation that never set requests up must not list a command that cannot work")
	}
	if issue3793Listed(issue3793Model("en-US", IntentRequestAvailable{})) {
		t.Error("a command the server has not answered about must not be listed")
	}
	m := issue3793Model("en-US", IntentRequestAvailable{Loaded: true, Available: true})
	m.IntentRequests.ConversationID = "elsewhere"
	if issue3793Listed(m) {
		t.Error("an answer about another conversation must not list the command")
	}
	m = issue3793Model("en-US", IntentRequestAvailable{Loaded: true, Available: true})
	m.Conversations[0].Agent = true
	if issue3793Listed(m) {
		t.Error("a conversation with an agent has no typed requests")
	}
	for _, c := range composerCommandsFor("thread-composer").commands {
		if c.Name == "leave" {
			t.Error("the reply box belongs to a message, not to a request")
		}
	}
}

func TestIssue3793_ChoosingLeaveConsumesTheLineAndOpensThePanel(t *testing.T) {
	m := issue3793Model("en-US", IntentRequestAvailable{Loaded: true, Available: true})
	opened, cleared := "", false
	m.IntentRequests.Open = func(command string) { opened = command }
	command, ok := composerCommandsFor("chat-composer").lookup(m, "leave")
	if !ok {
		t.Fatal("leave is not a command here")
	}
	command.Run(composerCommandRuntime{Model: m, ClearDraft: func() { cleared = true }}, "")
	if opened != "leave" || !cleared {
		t.Fatalf("opened %q cleared %v", opened, cleared)
	}
}

func TestIssue3793_FormShowsTypedLabelledFieldsWithTheirValues(t *testing.T) {
	m := issue3793Open(issue3793Model("en-US", IntentRequestAvailable{Loaded: true, Available: true}), IntentRequestForm)
	got := renderNode(t, intentRequestPanel(m))
	for _, want := range []string{"Ask for leave", `data-intent-request="leave"`, `name="employee"`, `name="starts"`, `name="ends"`, `name="approver"`, `name="reason"`, `type="date"`, "<textarea", "Maria Lopez", "(required)", "(optional)", "Review request", `type="submit"`, "<section"} {
		if !strings.Contains(got, want) {
			t.Errorf("form lacks %q: %s", want, got)
		}
	}
	for _, id := range []string{"employee", "starts", "ends", "approver", "reason"} {
		if !strings.Contains(got, `for="chat-request-`+id+`"`) {
			t.Errorf("field %s is not labelled", id)
		}
	}
	for _, bad := range []string{"hcmnext.workforce", "idempotency", "intent_id"} {
		if strings.Contains(got, bad) {
			t.Errorf("the form leaks %q: %s", bad, got)
		}
	}
	if !strings.Contains(got, "2026-12-01") || !strings.Contains(got, "Family trip") {
		t.Errorf("typed values are lost when the form is drawn again: %s", got)
	}
}

func TestIssue3793_RefusedFieldsAreNamedAndDescribed(t *testing.T) {
	m := issue3793Open(issue3793Model("en-US", IntentRequestAvailable{Loaded: true, Available: true}), IntentRequestForm)
	m.IntentRequests.View.Errors = map[string]string{"ends": chat.IntentRequestErrBadRange, "employee": chat.FormErrUnknownUser}
	got := renderNode(t, intentRequestPanel(m))
	for _, want := range []string{"The last day cannot be before the first day.", "Choose someone who is in this conversation.", "Some details need your attention.", `aria-invalid="true"`, `aria-describedby="chat-request-ends-error"`, `id="chat-request-ends-error"`, `role="alert"`} {
		if !strings.Contains(got, want) {
			t.Errorf("refused form lacks %q: %s", want, got)
		}
	}
}

func TestIssue3793_ReviewStepNeverImpliesApproval(t *testing.T) {
	m := issue3793Open(issue3793Model("en-US", IntentRequestAvailable{Loaded: true, Available: true}), IntentRequestReview)
	got := renderNode(t, intentRequestPanel(m))
	for _, want := range []string{"Check your request", "Maria Lopez", "Family trip", "Not given", "request for review to HR Assistant", "does not approve or start the leave", `data-request-action="send"`, `data-request-action="edit"`, "Send request"} {
		if !strings.Contains(got, want) {
			t.Errorf("review lacks %q: %s", want, got)
		}
	}
	if strings.Contains(got, "<form") || strings.Contains(strings.ToLower(got), "approved by") {
		t.Errorf("the review step is not a form and must not claim approval: %s", got)
	}
	busy := renderNode(t, intentRequestPanel(issue3793Open(issue3793Model("en-US", IntentRequestAvailable{Loaded: true, Available: true}), IntentRequestSending)))
	if !strings.Contains(busy, "Sending...") || !strings.Contains(busy, `aria-busy="true"`) || strings.Count(busy, "disabled") < 2 {
		t.Errorf("a request on its way cannot be sent twice: %s", busy)
	}
	m.IntentRequests.View.Notice = "failed"
	if failed := renderNode(t, intentRequestPanel(m)); !strings.Contains(failed, "could not be sent") || !strings.Contains(failed, `role="alert"`) {
		t.Errorf("a failed send is not said: %s", failed)
	}
}

func TestIssue3793_SentReceiptLinksTheRequestAndSaysItIsNotApproved(t *testing.T) {
	m := issue3793Open(issue3793Model("en-US", IntentRequestAvailable{Loaded: true, Available: true}), IntentRequestSent)
	m.IntentRequests.View.Href = "/workspace/app/journeys?journey=intent-1"
	got := renderNode(t, intentRequestPanel(m))
	for _, want := range []string{"Request sent for review", "It is not approved yet", "/workspace/app/journeys?journey=intent-1", "Open the request", `role="status"`} {
		if !strings.Contains(got, want) {
			t.Errorf("receipt lacks %q: %s", want, got)
		}
	}
}

// A receipt stays on screen even if a later read says requests are paused: the
// request was sent, and hiding that would invite sending it again.
func TestIssue3793_SentReceiptSurvivesALaterUnavailableRead(t *testing.T) {
	m := issue3793Open(issue3793Model("en-US", IntentRequestAvailable{Loaded: true, Reason: "paused"}), IntentRequestSent)
	m.IntentRequests.View.Href = "/workspace/app/journeys?journey=intent-1"
	got := renderNode(t, intentRequestPanel(m))
	if !strings.Contains(got, "Request sent for review") || !strings.Contains(got, "intent-1") {
		t.Fatalf("the receipt was hidden: %s", got)
	}
}

func TestIssue3793_UnavailableStateSaysWhyInPlainWords(t *testing.T) {
	for reason, want := range map[string]string{
		"paused":      "paused or removed",
		"not_granted": "has not been given permission",
		"not_allowed": "do not have permission",
		"surprise":    "not available here right now",
	} {
		m := issue3793Open(issue3793Model("en-US", IntentRequestAvailable{Loaded: true, Reason: reason}), IntentRequestForm)
		got := renderNode(t, intentRequestPanel(m))
		if !strings.Contains(got, want) || strings.Contains(got, "<form") || strings.Contains(got, `name="reason"`) {
			t.Errorf("%s: %s", reason, got)
		}
	}
}

func TestIssue3793_PanelOnlyShowsForTheConversationItWasOpenedIn(t *testing.T) {
	m := issue3793Open(issue3793Model("en-US", IntentRequestAvailable{Loaded: true, Available: true}), IntentRequestForm)
	m.IntentRequests.View.ConversationID = "elsewhere"
	if intentRequestPanel(m) != nil {
		t.Error("a request opened in another conversation must not follow the person here")
	}
	m = issue3793Open(issue3793Model("en-US", IntentRequestAvailable{Loaded: true, Available: true}), IntentRequestForm)
	m.IntentRequests.View.Command = "fire"
	if intentRequestPanel(m) != nil {
		t.Error("an unknown command draws nothing")
	}
}

func TestIssue3793_CopyExistsInEveryLocaleAndRightToLeftHolds(t *testing.T) {
	en := issue3793Copy["en-US"]
	for _, locale := range []string{"de-DE", "ar"} {
		for key, english := range en {
			if got := issue3793Copy[locale][key]; strings.TrimSpace(got) == "" || (got == english && !strings.HasPrefix(key, keyIntentReqErrPrefix)) {
				t.Errorf("%s has no translation of %s", locale, key)
			}
		}
		if len(issue3793Copy[locale]) != len(en) {
			t.Errorf("%s has %d strings, en-US has %d", locale, len(issue3793Copy[locale]), len(en))
		}
	}
	for _, command := range chat.IntentRequestCommands() {
		for _, f := range command.Fields {
			for _, locale := range []string{"en-US", "de-DE", "ar"} {
				if issue3793Copy[locale][keyIntentReqFieldPrefix+f.ID] == "" {
					t.Errorf("%s has no label for %s", locale, f.ID)
				}
			}
		}
	}
	m := issue3793Open(issue3793Model("ar", IntentRequestAvailable{Loaded: true, Available: true}), IntentRequestForm)
	got := renderNode(t, intentRequestPanel(m))
	if !strings.Contains(got, "طلب إجازة") || !strings.Contains(got, "مراجعة الطلب") || !strings.Contains(got, `dir="rtl"`) {
		t.Errorf("arabic panel: %s", got)
	}
	if IntentRequestText("de-DE", keyIntentReqSend) != "Antrag senden" {
		t.Error("german copy")
	}
	for _, want := range []string{"min-block-size:44px", "border-inline-start", "overflow-wrap:anywhere"} {
		if !strings.Contains(Issue3793Styles, want) {
			t.Errorf("styles lack %q", want)
		}
	}
	if strings.Contains(Issue3793Styles, "margin-left") || strings.Contains(Issue3793Styles, "padding-right") {
		t.Error("physical left/right properties break right-to-left layout")
	}
	if !strings.Contains(ChatLane3Styles, "chat-request-panel") {
		t.Error("the panel styles are not part of the page styles")
	}
}
