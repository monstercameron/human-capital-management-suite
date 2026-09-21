package journey

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestTodo_REV_095_01_Required pins the client-side rule the edit-proposal
// dialog uses: it is Field.Required, not a second per-field list, and a
// marked field renders aria-invalid with its message linked through
// aria-describedby.
func TestTodo_REV_095_01_Required(t *testing.T) {
	fields := []Field{
		{ID: "edit-grade", Name: "target_grade", Label: "Target grade", Required: true},
		{ID: "edit-note", Name: "note", Label: "Note"},
		{ID: "edit-token", Name: "token", Required: true, Kind: fieldKindHidden},
		{ID: "edit-date", Name: "effective_date", Label: "Effective date", Kind: fieldKindDate, Required: true},
	}
	missing := MissingRequired(fields, map[string]string{"effective_date": "2026-12-01", "target_grade": "  "})
	if len(missing) != 1 || missing[0].ID != "edit-grade" {
		t.Fatalf("missing = %+v, want only the blank required grade", missing)
	}
	if got := MissingRequired(fields, map[string]string{"target_grade": "M2", "effective_date": "2026-12-01"}); len(got) != 0 {
		t.Fatalf("a complete submit reported %+v", got)
	}

	marked := WithFieldErrors(fields, map[string]string{"edit-grade": "Complete this field."})
	if marked[0].Error != "Complete this field." || fields[0].Error != "" || marked[1].Error != "" {
		t.Fatalf("WithFieldErrors must mark only the named field, on a copy: %+v", marked)
	}
	if same := WithFieldErrors(fields, nil); len(same) != len(fields) {
		t.Fatal("no errors changed the field set")
	}

	markup, err := ui.RenderToString(fieldNode(live{}, marked[0], false))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`aria-invalid="true"`, `aria-describedby="edit-grade-error"`, `id="edit-grade-error"`, "Complete this field."} {
		if !strings.Contains(markup, want) {
			t.Fatalf("invalid field markup missing %q:\n%s", want, markup)
		}
	}
}
