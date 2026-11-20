package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func issue3795Message(t *testing.T) Message {
	t.Helper()
	body, err := chat.FormCardBody(chat.FormCard{
		Title: "Salary adjustment", Addressee: "maria", FormRef: "salary-adjustment", RunRef: "run-secret-1",
		Fields: []chat.FormField{
			{ID: "reason", Label: "Reason", Type: chat.FormFieldText, Required: true, MaxLength: 80},
			{ID: "notes", Label: "Notes", Type: chat.FormFieldText, MaxLength: 400},
			{ID: "effective", Label: "Effective date", Type: chat.FormFieldDate, Required: true},
			{ID: "code", Label: "Reason code", Type: chat.FormFieldSelect, Required: true, Options: []chat.FormOption{{Value: "merit", Label: "Merit"}, {Value: "market", Label: "Market"}}},
			{ID: "approver", Label: "Budget approver", Type: chat.FormFieldPerson},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return Message{ID: "card", AuthorID: "agent-1", Author: "Pay agent", Body: body, SentAt: time.Date(2026, 11, 19, 9, 41, 0, 0, time.UTC)}
}

func issue3795Model(locale, user string) Model {
	m := chatux021Model()
	m.Locale = locale
	m.CurrentUser = user
	m.Members = append(m.Members, Member{ID: "maria", HomeTenantID: "t", Name: "Maria Lopez"}, Member{ID: "sam", HomeTenantID: "t", Name: "Sam Reed"})
	return m
}

func TestIssue3795_AddresseeSeesTheTypedFieldsAndSubmit(t *testing.T) {
	m := issue3795Model("en-US", "maria")
	m.Forms.Views = map[string]FormCardView{"card": {Loaded: true, CanSubmit: true}}
	got := renderNode(t, message(m, handlers{}, issue3795Message(t), false))
	for _, want := range []string{
		"Salary adjustment", "Asked of you", `data-form-card="card"`,
		`name="reason"`, `name="notes"`, "<textarea", `type="date"`, `name="effective"`, `name="code"`, "Merit", "Market",
		`name="approver"`, "Sam Reed", "Choose...", "(required)", "Submit", `type="submit"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("addressee card lacks %q: %s", want, got)
		}
	}
	for _, bad := range []string{"form-card:", "run-secret-1", "salary-adjustment", "message-actions"} {
		if strings.Contains(got, bad) {
			t.Errorf("card leaks %q: %s", bad, got)
		}
	}
	// Every field is labelled for its input.
	for _, id := range []string{"form-card-card-reason", "form-card-card-effective", "form-card-card-code", "form-card-card-approver"} {
		if !strings.Contains(got, `for="`+id+`"`) || !strings.Contains(got, `id="`+id+`"`) {
			t.Errorf("field %s is not labelled", id)
		}
	}
}

func TestIssue3795_RefusedFieldsAreNamedAndDescribed(t *testing.T) {
	m := issue3795Model("en-US", "maria")
	m.Forms.Views = map[string]FormCardView{"card": {Loaded: true, CanSubmit: true, Errors: map[string]string{"effective": chat.FormErrBadDate, "reason": chat.FormErrRequired}, Notice: "invalid"}}
	got := renderNode(t, message(m, handlers{}, issue3795Message(t), false))
	for _, want := range []string{"Enter a real date.", "This answer is required.", "Some answers need your attention.", `aria-invalid="true"`, `aria-describedby="form-card-card-effective-error"`, `id="form-card-card-effective-error"`, `role="alert"`} {
		if !strings.Contains(got, want) {
			t.Errorf("refused card lacks %q: %s", want, got)
		}
	}
}

func TestIssue3795_BusyCardCannotBeSubmittedTwice(t *testing.T) {
	m := issue3795Model("en-US", "maria")
	m.Forms.Views = map[string]FormCardView{"card": {Loaded: true, CanSubmit: true, Busy: true}}
	got := renderNode(t, message(m, handlers{}, issue3795Message(t), false))
	if !strings.Contains(got, "Sending...") || !strings.Contains(got, `aria-busy="true"`) || strings.Count(got, "disabled") < 6 {
		t.Errorf("busy card: %s", got)
	}
}

// Someone else in the conversation sees who the form is for and whether it was
// answered, with no field and no answer.
func TestIssue3795_OtherReadersSeeOnlyWhoAndWhetherItIsAnswered_Security(t *testing.T) {
	m := issue3795Model("en-US", "sam")
	m.Forms.Views = map[string]FormCardView{"card": {Loaded: true}}
	waiting := renderNode(t, message(m, handlers{}, issue3795Message(t), false))
	for _, want := range []string{"Asked of Maria Lopez", "Waiting for Maria Lopez to answer."} {
		if !strings.Contains(waiting, want) {
			t.Errorf("waiting card lacks %q: %s", want, waiting)
		}
	}
	for _, bad := range []string{"<form", "<input", "<select", "Submit"} {
		if strings.Contains(waiting, bad) {
			t.Errorf("a bystander gets %q: %s", bad, waiting)
		}
	}
	m.Forms.Views = map[string]FormCardView{"card": {Loaded: true, Submitted: true, SubmittedAt: time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)}}
	done := renderNode(t, message(m, handlers{}, issue3795Message(t), false))
	if !strings.Contains(done, "Answered") || strings.Contains(done, "Your answers") || strings.Contains(done, "<form") {
		t.Errorf("answered card for a bystander: %s", done)
	}
}

func TestIssue3795_TheAddresseeSeesTheirOwnAnswersInWords(t *testing.T) {
	m := issue3795Model("en-US", "maria")
	m.Forms.Views = map[string]FormCardView{"card": {Loaded: true, Submitted: true, SubmittedAt: time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC),
		Mine: map[string]string{"reason": "Promotion", "effective": "2027-02-01", "code": "merit", "approver": "sam"}}}
	got := renderNode(t, message(m, handlers{}, issue3795Message(t), false))
	for _, want := range []string{"Answered", "Your answers", "Promotion", "Merit", "Sam Reed", "Not answered"} {
		if !strings.Contains(got, want) {
			t.Errorf("answered card lacks %q: %s", want, got)
		}
	}
	if strings.Contains(got, "<form") || strings.Contains(got, ">merit<") || strings.Contains(got, ">sam<") {
		t.Errorf("answered card still offers the form or shows raw values: %s", got)
	}
}

func TestIssue3795_UnloadedCardSaysSoInsteadOfOfferingAForm(t *testing.T) {
	m := issue3795Model("en-US", "maria")
	got := renderNode(t, message(m, handlers{}, issue3795Message(t), false))
	if !strings.Contains(got, "Checking this form...") || strings.Contains(got, "<form") {
		t.Errorf("unloaded card: %s", got)
	}
}

func TestIssue3795_CopyExistsInEveryLocaleAndRightToLeftHolds(t *testing.T) {
	en := issue3795Copy["en-US"]
	for _, locale := range []string{"de-DE", "ar"} {
		for key, english := range en {
			if got := issue3795Copy[locale][key]; strings.TrimSpace(got) == "" || (got == english && !strings.HasPrefix(key, keyIssue3795ErrPrefix)) {
				t.Errorf("%s has no translation of %s", locale, key)
			}
		}
		if len(issue3795Copy[locale]) != len(en) {
			t.Errorf("%s has %d strings, en-US has %d", locale, len(issue3795Copy[locale]), len(en))
		}
	}
	m := issue3795Model("ar", "maria")
	m.Forms.Views = map[string]FormCardView{"card": {Loaded: true, CanSubmit: true}}
	got := renderNode(t, message(m, handlers{}, issue3795Message(t), false))
	if !strings.Contains(got, "إرسال") || !strings.Contains(got, "موجّه إليك") || !strings.Contains(got, `dir="auto"`) {
		t.Errorf("arabic card: %s", got)
	}
	for _, want := range []string{"inline-size:min(100%,32rem)", "min-block-size:44px", "border-inline-start"} {
		if !strings.Contains(Issue3795Styles, want) {
			t.Errorf("styles lack %q", want)
		}
	}
	if strings.Contains(Issue3795Styles, "margin-left") || strings.Contains(Issue3795Styles, "padding-right") {
		t.Error("physical left/right properties break right-to-left layout")
	}
	if !strings.Contains(ChatLane3Styles, "chat-form-card") {
		t.Error("the card styles are not part of the page styles")
	}
}

// A form card has no author line of its own, so it must say who is asking, and
// it must never be grouped under the message before it nor swallow the next one.
func TestIssue3795_CardNamesTheAskerAndNeverGroupsWithMessages(t *testing.T) {
	m := issue3795Model("en-US", "maria")
	m.Forms.Views = map[string]FormCardView{"card": {Loaded: true, CanSubmit: true}}
	got := renderNode(t, message(m, handlers{}, issue3795Message(t), false))
	if !strings.Contains(got, "From Pay agent") {
		t.Errorf("the card does not name who asks: %s", got)
	}
	if !chatux021IsLine(issue3795Message(t)) {
		t.Error("the message after a form card would be grouped under it and lose its author line")
	}
	de := issue3795Model("de-DE", "maria")
	de.Forms.Views = m.Forms.Views
	if got := renderNode(t, message(de, handlers{}, issue3795Message(t), false)); !strings.Contains(got, "Von Pay agent") {
		t.Errorf("german card does not name who asks: %s", got)
	}
}

// Wherever a message shows as one line (Saved, previews) a form card reads as
// its title, never as its marker and field definitions.
func TestIssue3795_CardReadsAsItsTitleInOneLineSurfaces_Security(t *testing.T) {
	m := issue3795Model("en-US", "sam")
	line, ok := chatux021LineText(m, issue3795Message(t))
	if !ok || line != "Form: Salary adjustment" {
		t.Fatalf("one-line text = %q, %v", line, ok)
	}
	for _, bad := range []string{"form-card:", "run-secret-1", "salary-adjustment", "reason"} {
		if strings.Contains(line, bad) {
			t.Errorf("one-line text leaks %q", bad)
		}
	}
	if _, ok := chatux021LineText(m, Message{ID: "x", Body: "plain words"}); ok {
		t.Error("a plain message was read as a card")
	}
}
