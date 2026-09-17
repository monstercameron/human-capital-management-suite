package i18n

import (
	"errors"
	"testing"
)

func voiceCatalog() Catalog {
	return Catalog{
		"de-DE": {{Key: "leave.approval.notice", MeaningID: "leave.approval.notice.v1", Text: "Urlaub für {person} ist genehmigt", Kind: Success, Params: []Parameter{{Name: "person", Format: "text"}}}},
		"en-US": {{Key: "leave.approval.notice", MeaningID: "leave.approval.notice.v1", Text: "Leave for {person} is approved", Kind: Success, Params: []Parameter{{Name: "person", Format: "text"}}}},
	}
}

func TestTodo_UIPOLISH_007(t *testing.T) {
	inputs := []LocaleInput{{Locale: "en-US", Messages: voiceCatalog()["en-US"]}, {Locale: "de-DE", Messages: voiceCatalog()["de-DE"]}}
	catalog := CatalogFromInputs(inputs...)
	if err := catalog.Validate(); err != nil {
		t.Fatal(err)
	}
	got, err := catalog.Render("en-US", "leave.approval.notice", map[string]string{"person": "Sam"})
	if err != nil || got != "Leave for Sam is approved" {
		t.Fatalf("render = %q, %v", got, err)
	}
}

func TestTodo_UIPOLISH_007_Golden(t *testing.T) {
	got, err := voiceCatalog().Render("de-DE", "leave.approval.notice", map[string]string{"person": "Sam"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "Urlaub für Sam ist genehmigt" {
		t.Fatalf("golden copy = %q", got)
	}
}

func TestTodo_UIPOLISH_007_I18N(t *testing.T) {
	bad := voiceCatalog()
	bad["de-DE"][0].MeaningID = "different.meaning.v2"
	if !errors.Is(bad.Validate(), ErrLocaleMismatch) {
		t.Fatalf("meaning drift accepted: %v", bad.Validate())
	}
	bad = voiceCatalog()
	bad["de-DE"][0].Text = "Urlaub ist genehmigt"
	if !errors.Is(bad.Validate(), ErrInvalidMessage) {
		t.Fatalf("placeholder drift accepted: %v", bad.Validate())
	}
}

func TestTodo_UIPOLISH_007_Accessibility(t *testing.T) {
	for _, kind := range []MessageKind{Error, Refusal, Recovery} {
		m := Message{Key: "task." + string(kind), MeaningID: "task." + string(kind) + ".v1", Text: "We could not save this", Kind: kind}
		if !errors.Is(m.Validate(), ErrInvalidMessage) {
			t.Fatalf("%s without recovery action accepted", kind)
		}
	}
}

func TestTodo_UIPOLISH_007_Regression(t *testing.T) {
	unknown := Message{Key: "task.unknown", MeaningID: "task.unknown.v1", Text: "A status", Kind: MessageKind("toast")}
	if !errors.Is(unknown.Validate(), ErrInvalidMessage) {
		t.Fatal("unsupported message kind accepted")
	}
	m := Message{Key: "task.save.button", MeaningID: "task.save.button.v1", Text: "Continue", Kind: Button, NextAction: "Save changes"}
	if !errors.Is(m.Validate(), ErrInvalidMessage) {
		t.Fatal("ambiguous Continue button accepted")
	}
	m = Message{Key: "task.save.button", MeaningID: "task.save.button.v1", Text: "Save changes", Kind: Button, NextAction: "Save changes"}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_UIPOLISH_007_Security(t *testing.T) {
	m := Message{Key: "task.status", MeaningID: "task.status.v1", Text: "JourneyService completed", Kind: Status}
	if !errors.Is(m.Validate(), ErrInvalidMessage) {
		t.Fatal("implementation vocabulary accepted")
	}
	if _, err := voiceCatalog().Render("en-US", "leave.approval.notice", nil); !errors.Is(err, ErrMissingParameter) {
		t.Fatalf("missing parameter = %v", err)
	}
}

func TestTodo_UIPOLISH_007_ParameterOrderIsSemantic(t *testing.T) {
	catalog := Catalog{
		"ar": {{Key: "pay.review.summary", MeaningID: "pay.review.summary.v1", Text: "المبلغ {amount} في {date}", Kind: Status,
			Params: []Parameter{{Name: "amount", Format: "money"}, {Name: "date", Format: "date"}}}},
		"de-DE": {{Key: "pay.review.summary", MeaningID: "pay.review.summary.v1", Text: "Betrag {amount} am {date}", Kind: Status,
			Params: []Parameter{{Name: "date", Format: "date"}, {Name: "amount", Format: "money"}}}},
		"en-US": {{Key: "pay.review.summary", MeaningID: "pay.review.summary.v1", Text: "Amount {amount} on {date}", Kind: Status,
			Params: []Parameter{{Name: "amount", Format: "money"}, {Name: "date", Format: "date"}}}},
	}
	if err := catalog.Validate(); err != nil {
		t.Fatalf("equivalent structured parameters rejected: %v", err)
	}
	got, err := catalog.Render("en-US", "pay.review.summary", map[string]string{"amount": "USD 1,234.50", "date": "2026-09-13"})
	if err != nil || got != "Amount USD 1,234.50 on 2026-09-13" {
		t.Fatalf("exact money/date values were changed: %q, %v", got, err)
	}
}

func TestTodo_UIPOLISH_007_RenderRejectsUndeclaredParameters(t *testing.T) {
	params := map[string]string{"person": "Sam", "unexpected": "leak"}
	if _, err := voiceCatalog().Render("en-US", "leave.approval.notice", params); !errors.Is(err, ErrMissingParameter) {
		t.Fatalf("undeclared parameter accepted: %v", err)
	}
}

func TestTodo_UIPOLISH_007_RenderDoesNotExpandParameterValues(t *testing.T) {
	cases := []struct {
		name      string
		nameOrder []Parameter
	}{
		{name: "placeholder-valued-parameter-first", nameOrder: []Parameter{{Name: "person", Format: "text"}, {Name: "date", Format: "date"}}},
		{name: "placeholder-valued-parameter-last", nameOrder: []Parameter{{Name: "date", Format: "date"}, {Name: "person", Format: "text"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			values := map[string]string{"person": "{date}", "date": "2026-09-13"}
			catalog := Catalog{"en-US": {{Key: "task.notice", MeaningID: "task.notice.v1", Text: "{person} on {date}", Kind: Status, Params: tc.nameOrder}}}
			got, err := catalog.Render("en-US", "task.notice", values)
			if err != nil || got != "{date} on 2026-09-13" {
				t.Fatalf("parameter value expanded with order %#v: %q, %v", tc.nameOrder, got, err)
			}
		})
	}
}

func TestTodo_UIPOLISH_007_RejectsVagueControlsAndImplementationCopy(t *testing.T) {
	for _, text := range []string{"Continue", "Submit", "Save", "OK"} {
		m := Message{Key: "task.action", MeaningID: "task.action.v1", Text: text, Kind: Button, NextAction: "Complete the request"}
		if !errors.Is(m.Validate(), ErrInvalidMessage) {
			t.Errorf("vague button %q accepted", text)
		}
	}
	for _, text := range []string{"Open the authenticated cell", "Retry the protobuf RPC adapter"} {
		m := Message{Key: "task.status", MeaningID: "task.status.v1", Text: text, Kind: Status}
		if !errors.Is(m.Validate(), ErrInvalidMessage) {
			t.Errorf("implementation copy %q accepted", text)
		}
	}
}

func TestTodo_UIPOLISH_007_HelperTextStaysProgressive(t *testing.T) {
	long := Message{Key: "task.field.help", MeaningID: "task.field.help.v1", Kind: Helper,
		Text: "This helper explains the field in exhaustive detail across several sentences, restating the task, the policy background, the review chain and the appeal route instead of staying a short progressive hint."}
	if !errors.Is(long.Validate(), ErrInvalidMessage) {
		t.Fatal("helper text longer than the task it explains was accepted")
	}
	short := Message{Key: "task.field.help", MeaningID: "task.field.help.v1", Kind: Helper,
		Text: "Use the worker's current base pay.", NextAction: ""}
	if err := short.Validate(); err != nil {
		t.Fatalf("concise helper was rejected: %v", err)
	}
}
