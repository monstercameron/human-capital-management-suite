package productui

import (
	"strings"
	"testing"
)

func TestTodo_UXAUDIT_021(t *testing.T) {
	view := testView(PageWorkerIDs)
	view.WorkerIDPolicy = WorkerIDPolicy{Version: 4, Prefix: "HC", SequenceDigits: 6, StartAt: 10, IncrementBy: 1}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`data-field-group="worker-id-identity"`, `data-field-group="worker-id-sequence"`,
		`data-field-group="worker-id-format"`, `data-field-group="worker-id-reserved"`,
		`data-unsaved-protection="true"`, `data-unsaved-form="worker-id"`,
		`worker-id-preview`, `aria-live="polite"`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("worker ID editor missing %q", want)
		}
	}
}

func TestTodo_UXAUDIT_021_Accessibility(t *testing.T) {
	view := testView(PageWorkerIDs)
	view.WorkerIDPolicy = WorkerIDPolicy{Version: 4, Prefix: "HC", SequenceDigits: 6, StartAt: 10, IncrementBy: 1}
	view.WorkerIDValidation = ValidationState{SubmissionAttempted: true, Issues: []ValidationIssue{{FieldID: "worker-prefix", MessageKey: "validation.required"}}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<fieldset class="admin-form-section" data-field-group="worker-id-identity">`,
		`<legend>`,
		`for="worker-prefix"`,
		`id="worker-prefix"`, `aria-describedby="worker-prefix-help worker-prefix-error"`,
		`id="worker-id-validation-summary"`, `href="#worker-prefix"`,
		`class="surface worker-id-preview"`, `aria-live="polite"`,
		`class="worker-id-actions sticky-actions"`,
		`aria-describedby="worker-id-status"`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("worker ID editor missing accessible form behavior %q", want)
		}
	}
}

func TestTodo_UXAUDIT_021_I18N(t *testing.T) {
	for _, locale := range []string{"de-DE", "ar"} {
		view := testView(PageWorkerIDs)
		view.Locale = ResolveProductLocale(locale)
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "Dash ( - )") || strings.Contains(doc, "Pad with leading zeroes") {
			t.Errorf("%s editor leaked English select options", locale)
		}
	}
}

func TestTodo_UXAUDIT_021_Regression(t *testing.T) {
	view := testView(PageWorkerIDs)
	view.WorkerIDPolicy = WorkerIDPolicy{SequenceDigits: 0, StartAt: 1, IncrementBy: 1}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `disabled`) || !strings.Contains(doc, "Complete the numeric fields") {
		t.Fatal("invalid authoritative numeric state must remain blocked with recovery guidance")
	}
	if strings.Contains(doc, "Atomic uniqueness") {
		t.Fatal("invalid draft must not present a successful preview")
	}
}
