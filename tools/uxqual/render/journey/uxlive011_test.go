package journey

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// UXLIVE-011: the journey page renders the target position with
// productui.PositionPicker rather than drawing a second version of it. What
// belongs here, and only here, is the layout: this page had no rules for the
// borrowed control at all, so its title and three labels rendered as one
// unbroken run -- "Sales DirectorSalesBoston, MAOpen now — Available" on the
// running server. A page that borrows a component and does not lay it out is
// the defect; the component is not.

func uxlive011Render(t *testing.T, f Field) string {
	t.Helper()
	markup, err := ui.RenderToString(fieldNode(live{locale: "en-US"}, f, false))
	if err != nil {
		t.Fatalf("render field: %v", err)
	}
	return markup
}

func uxlive011PickerField(vacancies ...VacancyOption) Field {
	return Field{
		ID: "propose-position", Name: "target_position_id", Kind: fieldKindPositionPicker,
		Label: "Target position (optional)", Help: "Every position listed is open and cleared for you to target.",
		Vacancies:  vacancies,
		EmptyTitle: "No open position to target", EmptyDetail: "Nothing open matches this role right now.",
	}
}

// The declared UXLIVE-011 matrix (PRIMARY/BROWSER/SECURITY) lives in
// tools/uxqual/journeyclient, where the choice is built. These are this
// package's own tests for the same todo's rendering half, named apart so the
// todo has exactly one test of each declared name.
//
// TestPositionPickerFieldRendersAGovernedChoice: a governed fieldset of
// radios carrying server-issued references, with no text input in it.
func TestPositionPickerFieldRendersAGovernedChoice(t *testing.T) {
	markup := uxlive011Render(t, uxlive011PickerField(
		VacancyOption{Reference: "eref:v1:t:position:abc.rev:v1:x", Title: "Sales Director",
			Organization: "Sales", Location: "Boston, MA", ReservationState: "AVAILABLE"},
	))

	if strings.Contains(markup, `type="text"`) {
		t.Fatalf("the target position still renders a text input:\n%s", markup)
	}
	if !strings.Contains(markup, "<fieldset") || !strings.Contains(markup, "<legend") {
		t.Fatalf("the picker is not a labelled group:\n%s", markup)
	}
	if !strings.Contains(markup, `type="radio"`) {
		t.Fatalf("the picker offers no selectable option:\n%s", markup)
	}
	if !strings.Contains(markup, `value="eref:v1:t:position:abc.rev:v1:x"`) {
		t.Fatalf("the option does not carry the server-issued reference:\n%s", markup)
	}
	if !strings.Contains(markup, "aria-describedby=\"position-picker-desc-") {
		t.Fatalf("the option's availability is not announced with it:\n%s", markup)
	}
	if !strings.Contains(markup, "Open now") || !strings.Contains(markup, "Available") {
		t.Fatalf("the option discloses neither its vacancy window nor its reservation state:\n%s", markup)
	}
	// The field's help still renders, and still under the id the error
	// summary and aria-describedby wiring elsewhere point at.
	if !strings.Contains(markup, `id="propose-position-help"`) {
		t.Fatalf("the picker lost its help text:\n%s", markup)
	}
}

// TestPositionPickerFieldIsLaidOut is the layout the live page was missing:
// the option's own labels have to be separable, and the classes the
// stylesheet addresses have to be the ones the markup actually carries.
func TestPositionPickerFieldIsLaidOut(t *testing.T) {
	markup := uxlive011Render(t, uxlive011PickerField(
		VacancyOption{Reference: "ref-1", Title: "Sales Director", Organization: "Sales",
			Location: "Boston, MA", ReservationState: "AVAILABLE"},
	))
	for _, class := range []string{"position-picker", "position-picker-options", "position-picker-option",
		"position-picker-option-main", "position-picker-option-meta"} {
		if !strings.Contains(markup, class) {
			t.Errorf("the rendered picker carries no %q for the stylesheet to address:\n%s", class, markup)
		}
	}

	sheet := Stylesheet()
	for _, rule := range []string{".position-picker{", ".position-picker-options{", ".position-picker-option{",
		".position-picker-option-main{", ".position-picker-option-meta{"} {
		if !strings.Contains(sheet, rule) {
			t.Errorf("the journey sheet does not lay out %q, so its labels run together", rule)
		}
	}
	// The labels are separated by layout, not by punctuation a screen reader
	// would also read out.
	if !strings.Contains(sheet, ".position-picker-option-main>small:empty{display:none") {
		t.Fatalf("an unrecorded label still claims its own gap")
	}
	if !strings.Contains(sheet, ".position-picker-empty{") || !strings.Contains(sheet, ".position-picker-empty-title{") {
		t.Fatalf("the no-vacancy state is unstyled")
	}
}

// TestPositionPickerFieldHasNoFreeTextPath is the property the whole todo
// rests on, at the last layer before the browser: there is no way to submit
// a position this page did not render. An empty list is the strongest case,
// because that is exactly where a free-text fallback would have been
// tempting.
func TestPositionPickerFieldHasNoFreeTextPath(t *testing.T) {
	empty := uxlive011Render(t, uxlive011PickerField())

	if strings.Contains(empty, "<input") {
		t.Fatalf("a picker with nothing to offer still rendered an input:\n%s", empty)
	}
	if !strings.Contains(empty, "position-picker-empty") {
		t.Fatalf("an empty picker vanished instead of explaining itself:\n%s", empty)
	}
	if !strings.Contains(empty, "No open position to target") || !strings.Contains(empty, "Nothing open matches this role") {
		t.Fatalf("the no-vacancy state says nothing:\n%s", empty)
	}
	if !strings.Contains(empty, "<legend") {
		t.Fatalf("an empty picker is an unlabelled region:\n%s", empty)
	}

	// An option whose reference is empty would submit as "no position" while
	// looking like a choice. The projector drops those before they get here;
	// this asserts the renderer does not reintroduce one by rendering a
	// value-less radio for a blank option.
	blank := uxlive011Render(t, uxlive011PickerField(VacancyOption{Title: "Unnamed"}))
	if strings.Contains(blank, `checked`) {
		t.Fatalf("an option with no reference was rendered as chosen:\n%s", blank)
	}
}
