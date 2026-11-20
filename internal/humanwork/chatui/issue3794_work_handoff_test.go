package chatui

import (
	"strings"
	"testing"
)

func issue3794Model(locale, role, status string) (Model, Message) {
	m, msg := issue3790Model(locale)
	m.Members = []Member{{ID: "walt", Name: "Walt Lead"}, {ID: "priya", Name: "Priya Rao"}, {ID: "bot", Name: "Helper", Agent: true}}
	view := WorkHandoffView{ID: "wh-1", PostID: "m1", ProjectID: "ops", TaskID: "t-1", Mode: "FOLLOW_UP", Role: role, FromID: "walt", ToID: "priya",
		NextAction: "Confirm the payroll correction", Note: "Needed before Friday", Status: status, OfferedOwner: "walt", OfferedDue: "2026-12-04"}
	if role == "recipient" {
		m.CurrentUser = "priya"
	}
	m.WorkHandoff = WorkHandoffState{Enabled: true, ByTask: map[string][]WorkHandoffView{"t-1": {view}}, Offer: func(string, string, string, string) {}, Answer: func(string, string) {}}
	return m, msg
}

// Issue 3794: the sender sees the offer with its action, recipient, source
// work, audience and a way to take it back; nothing else.
func TestIssue3794_SenderSeesTheReviewableOfferAndCanWithdraw(t *testing.T) {
	m, msg := issue3794Model("en-US", "sender", "OFFERED")
	got := renderNode(t, spanOf(chatworkChips(m, msg)))
	for _, want := range []string{"You offered this to Priya Rao", "Waiting for Priya Rao", "Confirm the payroll correction", "Needed before Friday", "Owner when offered", "You", "Due when offered", "2026-12-04",
		"Only you and Priya Rao can see this offer", "Withdraw offer", `data-action="handoff-withdraw"`, `data-id="wh-1"`, `role="group"`, "Take the next step"} {
		if !strings.Contains(got, want) {
			t.Errorf("sender card lacks %q: %s", want, got)
		}
	}
	for _, bad := range []string{`data-action="handoff-accept"`, `data-action="handoff-decline"`} {
		if strings.Contains(got, bad) {
			t.Errorf("sender is offered %s", bad)
		}
	}
	// A follow-up says that nothing else about the work changes.
	if !strings.Contains(got, "keeps its owner, deadline and history") || !strings.Contains(got, "nobody gains approval rights") {
		t.Errorf("follow-up effect is not stated: %s", got)
	}
}

func TestIssue3794_RecipientCanAcceptOrDeclineAndTransferSaysWhatHappens(t *testing.T) {
	m, msg := issue3794Model("en-US", "recipient", "OFFERED")
	view := m.WorkHandoff.ByTask["t-1"][0]
	view.Mode, view.Changed = "TRANSFER", true
	m.WorkHandoff.ByTask["t-1"] = []WorkHandoffView{view}
	got := renderNode(t, spanOf(chatworkChips(m, msg)))
	for _, want := range []string{"Waiting for your answer", `data-action="handoff-accept"`, `data-action="handoff-decline"`, "Take over the work",
		"checks their access again", "Its deadline and history are kept", "has changed since this was offered", `role="alert"`} {
		if !strings.Contains(got, want) {
			t.Errorf("recipient card lacks %q: %s", want, got)
		}
	}
	if !strings.Contains(got, "Walt Lead offered you the next step") || strings.Contains(got, "handoff-withdraw") {
		t.Errorf("recipient card is wrong: %s", got)
	}
}

func TestIssue3794_AnsweredOffersShowTheRealStateWithoutButtons(t *testing.T) {
	for status, want := range map[string]string{"ACCEPTED": "Accepted", "DECLINED": "Declined", "WITHDRAWN": "Withdrawn"} {
		m, msg := issue3794Model("en-US", "recipient", status)
		got := renderNode(t, spanOf(chatworkChips(m, msg)))
		if !strings.Contains(got, want) || strings.Contains(got, `data-action="handoff-accept"`) || strings.Contains(got, "chathandoff-changed") {
			t.Errorf("%s: %s", status, got)
		}
	}
}

func TestIssue3794_HandOffActionIsForTheOwnerWhereTheServerKeepsOffers(t *testing.T) {
	m, msg := issue3794Model("en-US", "sender", "ACCEPTED")
	if got := renderNode(t, spanOf(chatworkChips(m, msg))); !strings.Contains(got, `data-action="handoff-open"`) || !strings.Contains(got, "Hand off") {
		t.Fatalf("owner has no Hand off action: %s", got)
	}
	for name, mutate := range map[string]func(*Model){
		"server keeps no offers": func(m *Model) { m.WorkHandoff.Enabled = false },
		"no form to open":        func(m *Model) { m.WorkHandoff.Offer = nil },
		"not the owner":          func(m *Model) { m.MessageWork.Links["m1"][0].OwnerID = "priya" },
		"nobody to offer it to": func(m *Model) {
			m.Members = []Member{{ID: "walt", Name: "Walt Lead"}, {ID: "bot", Name: "Helper", Agent: true}}
		},
	} {
		other, otherMsg := issue3794Model("en-US", "sender", "ACCEPTED")
		mutate(&other)
		if strings.Contains(renderNode(t, spanOf(chatworkChips(other, otherMsg))), "handoff-open") {
			t.Errorf("%s: Hand off is offered", name)
		}
	}
	// An offer about other work, or from another message, is not shown here.
	m.WorkHandoff.ByTask["t-1"][0].PostID = "m9"
	if got := renderNode(t, spanOf(chatworkChips(m, msg))); strings.Contains(got, "chathandoff ") || strings.Contains(got, "Confirm the payroll") {
		t.Errorf("an offer for another message appears: %s", got)
	}
}

func TestIssue3794_ButtonsCallBackAndBusyOfferCannotBeAnsweredTwice(t *testing.T) {
	var calls []string
	m, _ := issue3794Model("en-US", "recipient", "OFFERED")
	m.WorkHandoff.Offer = func(post, project, task, title string) {
		calls = append(calls, "offer:"+post+"/"+project+"/"+task+"/"+title)
	}
	m.WorkHandoff.Answer = func(id, verb string) { calls = append(calls, verb+":"+id) }
	m.actWith("handoff-open", "t-1", "")
	m.actWith("handoff-accept", "wh-1", "")
	m.actWith("handoff-decline", "wh-1", "")
	m.WorkHandoff.Busy = map[string]bool{"wh-1": true}
	m.actWith("handoff-withdraw", "wh-1", "")
	want := "offer:m1/ops/t-1/Swap Dana's laptop,accept:wh-1,decline:wh-1"
	if strings.Join(calls, ",") != want {
		t.Fatalf("calls = %v", calls)
	}
	busy, msg := issue3794Model("en-US", "recipient", "OFFERED")
	busy.WorkHandoff.Busy = map[string]bool{"wh-1": true}
	if got := renderNode(t, spanOf(chatworkChips(busy, msg))); !strings.Contains(got, "disabled") || !strings.Contains(got, `aria-busy="true"`) {
		t.Errorf("a busy offer can be answered again: %s", got)
	}
}

func TestIssue3794_GermanAndArabicCopyAndEveryKeyIsTranslated(t *testing.T) {
	for _, locale := range []string{"de-DE", "ar"} {
		for key, en := range workHandoffCopy["en-US"] {
			got := workHandoffCopy[locale][key]
			if strings.TrimSpace(got) == "" || got == en {
				t.Errorf("%s: key %q is not translated", locale, key)
			}
			if strings.Count(en, "{name}") != strings.Count(got, "{name}") {
				t.Errorf("%s: key %q lost its {name} placeholder", locale, key)
			}
		}
	}
	m, msg := issue3794Model("de-DE", "recipient", "OFFERED")
	if got := renderNode(t, spanOf(chatworkChips(m, msg))); !strings.Contains(got, "Annehmen") || !strings.Contains(got, "Wartet auf Ihre Antwort") || strings.Contains(got, "Waiting for your answer") {
		t.Errorf("German card: %s", got)
	}
	m, msg = issue3794Model("ar", "recipient", "OFFERED")
	if got := renderNode(t, spanOf(chatworkChips(m, msg))); !strings.Contains(got, "قبول") || !strings.Contains(got, "بانتظار ردك") {
		t.Errorf("Arabic card: %s", got)
	}
}

func TestIssue3794_DialogReviewFieldsKeyboardAndRTL(t *testing.T) {
	v := WorkHandoffDialogView{Locale: "en-US", PostID: "m1", ProjectID: "ops", TaskID: "t-1", Title: "Fix March payroll", Mode: "TRANSFER", RecipientID: "priya", NextAction: "Confirm it",
		People: []WorkHandoffPerson{{ID: "priya", Name: "Priya Rao"}, {ID: "sam", Name: "Sam"}}}
	got := renderNode(t, RenderWorkHandoffDialog(v))
	for _, want := range []string{`role="dialog"`, `aria-modal="true"`, `aria-labelledby="chathandoff-title"`, "Hand off this work: Fix March payroll", "<fieldset", "<legend>What are you offering?</legend>",
		`type="radio"`, "Take the next step", "Take over the work", "They become the owner if they accept", `id="chathandoff-field-to"`, "Priya Rao", `id="chathandoff-field-action"`, `maxlength="200"`,
		`id="chathandoff-field-note"`, `maxlength="1000"`, "Send offer", `data-chathandoff-close="true"`, `dir="ltr"`} {
		if !strings.Contains(got, want) {
			t.Errorf("dialog lacks %q: %s", want, got)
		}
	}
	v.Locale, v.Error, v.Busy = "ar", "conflict", true
	got = renderNode(t, RenderWorkHandoffDialog(v))
	for _, want := range []string{`dir="rtl"`, "جارٍ إرسال العرض", `role="alert"`, `aria-busy="true"`, "disabled"} {
		if !strings.Contains(got, want) {
			t.Errorf("Arabic busy dialog lacks %q: %s", want, got)
		}
	}
	// With nobody to offer it to the form says so instead of an empty select.
	v = WorkHandoffDialogView{Locale: "en-US", Title: "Fix March payroll"}
	if got = renderNode(t, RenderWorkHandoffDialog(v)); !strings.Contains(got, "nobody else in this conversation") || strings.Contains(got, "<form") {
		t.Errorf("empty dialog: %s", got)
	}
}

func TestIssue3794_ErrorsAreSaidInPlainWordsInEveryLanguage(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		seen := map[string]bool{}
		for _, code := range []string{"invalid_argument", "not_found", "permission_denied", "conflict", "unavailable", "action", "person"} {
			text := WorkHandoffErrorText(locale, code)
			if strings.TrimSpace(text) == "" || seen[text] || strings.Contains(text, code) {
				t.Errorf("%s %s: %q", locale, code, text)
			}
			seen[text] = true
		}
	}
	if WorkHandoffErrorText("en-US", "") != "" {
		t.Error("no error should be no text")
	}
}

func TestIssue3794_CandidatesAreOtherPeopleInTheConversation(t *testing.T) {
	m, _ := issue3794Model("en-US", "sender", "OFFERED")
	got := WorkHandoffCandidates(m)
	if len(got) != 1 || got[0].ID != "priya" {
		t.Fatalf("candidates = %+v", got)
	}
}
