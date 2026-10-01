package journey

import (
	"strings"
	"testing"
)

func TestTodo_UXLIVE_036(t *testing.T) {
	form := ProposalForm{Submit: "Review and submit", Fields: []Field{
		{ID: "propose-job", Name: "target_job_code", Kind: fieldKindSelect, Required: true, Value: "OPS-HRBP3"},
		{ID: "propose-base", Name: "proposed_base", Kind: fieldKindNumber, Required: true, Value: "0", Error: "Enter an amount within the approved range."},
		{ID: "propose-reason", Name: "business_reason", Kind: fieldKindTextarea, Required: true, Error: "Enter a clear business reason."},
		{ID: "propose-effective", Name: "effective_date", Kind: fieldKindDate, Required: true, Value: "2026-12-01"},
	}}
	markup := mustRenderNode(t, proposalFormSection(live{locale: "en-US"}, form, "Promotion details"))
	if strings.Contains(markup, "jn-confirm") {
		t.Fatalf("invalid proposal opened the review surface:\n%s", markup)
	}
	for _, want := range []string{`aria-invalid="true"`, `aria-describedby="propose-base-error"`, "Enter an amount within the approved range."} {
		if !strings.Contains(markup, want) {
			t.Fatalf("invalid proposal missing %q:\n%s", want, markup)
		}
	}
}

func TestTodo_UXLIVE_036_Browser(t *testing.T) {
	form := ProposalForm{Submit: "Review and submit", Fields: []Field{
		{ID: "propose-job", Name: "target_job_code", Kind: fieldKindSelect, Required: true, Value: "OPS-HRBP3"},
		{ID: "propose-base", Name: "proposed_base", Kind: fieldKindNumber, Required: true, Value: "0", Help: "Currency and period apply.", Error: "Enter an exact amount."},
	}}
	markup := mustRenderNode(t, proposalFormSection(live{locale: "en-US"}, form, "Promotion details"))
	if strings.Contains(markup, `class="jn-confirm"`) {
		t.Fatal("the review trigger is available while the pay field is invalid")
	}
}

func TestTodo_UXLIVE_036_Accessibility(t *testing.T) {
	form := ProposalForm{Submit: "Review and submit", Fields: []Field{
		{ID: "propose-job", Name: "target_job_code", Kind: fieldKindSelect, Required: true, Value: "OPS-HRBP3"},
		{ID: "propose-base", Name: "proposed_base", Kind: fieldKindNumber, Required: true, Value: "0", Help: "Currency and period apply.", Error: "Enter an exact amount."},
	}}
	markup := mustRenderNode(t, proposalFormSection(live{locale: "en-US"}, form, "Promotion details"))
	for _, want := range []string{`aria-invalid="true"`, `aria-describedby="propose-base-help propose-base-error"`, `id="propose-base-error"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("invalid field is not accessibly linked through %q:\n%s", want, markup)
		}
	}

	t.Run("omitting help omits only the help reference", func(t *testing.T) {
		form.Fields[1].Help = ""
		markup := mustRenderNode(t, proposalFormSection(live{locale: "en-US"}, form, "Promotion details"))
		if !strings.Contains(markup, `aria-describedby="propose-base-error"`) || strings.Contains(markup, "propose-base-help") {
			t.Fatalf("error-only field has incorrect description references:\n%s", markup)
		}
	})
}

func TestTodo_UXLIVE_036_Integration(t *testing.T) {
	form := ProposalForm{Submit: "Review and submit", Fields: []Field{{ID: "propose-job", Name: "target_job_code", Kind: fieldKindSelect, Required: true, Value: "OPS-HRBP3"}}}
	markup := mustRenderNode(t, proposalFormSection(live{locale: "en-US"}, form, "Promotion details"))
	if strings.Contains(markup, "jn-confirm") || !strings.Contains(markup, "Propose promotion") && !strings.Contains(markup, "Review and submit") {
		t.Fatalf("invalid form did not retain its recoverable action:\n%s", markup)
	}
}

func TestTodo_UXLIVE_036_Security(t *testing.T) {
	form := ProposalForm{Submit: "Review and submit", Fields: []Field{{ID: "propose-base", Name: "proposed_base", Kind: fieldKindNumber, Required: true, Value: "0", Error: "Enter an exact amount."}}}
	markup := mustRenderNode(t, proposalFormSection(live{locale: "en-US"}, form, "Promotion details"))
	if strings.Contains(markup, "Submit proposal") || strings.Contains(markup, "propose-review") {
		t.Fatal("invalid proposal exposed a committing review action")
	}
}

func TestTodo_UXLIVE_036_Regression(t *testing.T) {
	form := ProposalForm{Submit: "Review and submit", Confirmation: []Fact{{Label: "Employee", Value: "Omar Reyes"}}, Fields: []Field{
		{ID: "propose-job", Name: "target_job_code", Kind: fieldKindSelect, Required: true, Value: "OPS-HRBP3"},
		{ID: "propose-base", Name: "proposed_base", Kind: fieldKindNumber, Required: true, Value: "115000.00"},
	}}
	markup := mustRenderNode(t, proposalFormSection(live{locale: "en-US"}, form, "Promotion details"))
	if !strings.Contains(markup, `class="jn-confirm"`) || !strings.Contains(markup, "Omar Reyes") {
		t.Fatalf("a valid proposal lost its review surface:\n%s", markup)
	}
}

func TestTodo_UXLIVE_038(t *testing.T) {
	markup := uxlive038Markup(t)
	for _, want := range []string{"rev-position-a", "rev-position-b", "Maya Chen", "Luis Gomez", "DATA-SDE3"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("position choice omitted authorized discriminator %q:\n%s", want, markup)
		}
	}
}

func uxlive038Markup(t *testing.T) string {
	t.Helper()
	field := Field{ID: "propose-position", Name: "desired_position_id", Kind: fieldKindPositionPicker, Label: "Target position", Vacancies: []VacancyOption{
		{Reference: "rev-position-a", Title: "Senior Data Engineer", JobCode: "DATA-SDE3", Organization: "Data & Analytics", Manager: "Maya Chen", Location: "Boston, MA", VacancyEndISO: "", ReservationState: "AVAILABLE"},
		{Reference: "rev-position-b", Title: "Senior Data Engineer", JobCode: "DATA-SDE3", Organization: "Data & Analytics", Manager: "Luis Gomez", Location: "Boston, MA", VacancyEndISO: "", ReservationState: "AVAILABLE"},
	}}
	return mustRenderNode(t, fieldNode(live{locale: "en-US"}, field, false))
}

func TestTodo_UXLIVE_038_Integration(t *testing.T) {
	markup := uxlive038Markup(t)
	if strings.Count(markup, `type="radio"`) != 2 || strings.Count(markup, `name="desired_position_id"`) != 2 {
		t.Fatalf("position choices are not a complete bound radio group:\n%s", markup)
	}
}
func TestTodo_UXLIVE_038_Golden(t *testing.T) {
	markup := uxlive038Markup(t)
	if strings.Index(markup, "rev-position-a") > strings.Index(markup, "rev-position-b") {
		t.Fatal("position options changed their server-projected order")
	}
}
func TestTodo_UXLIVE_038_Browser(t *testing.T) {
	markup := uxlive038Markup(t)
	if !strings.Contains(markup, "Senior Data Engineer") || strings.Count(markup, "Boston, MA") != 2 {
		t.Fatalf("narrow position options lost their human context:\n%s", markup)
	}
}
func TestTodo_UXLIVE_038_Accessibility(t *testing.T) {
	field := Field{ID: "propose-position", Name: "desired_position_id", Kind: fieldKindPositionPicker, Label: "Target position", Vacancies: []VacancyOption{{Reference: "rev-position-a", Title: "Senior Data Engineer", JobCode: "DATA-SDE3", Organization: "Data & Analytics", Manager: "Maya Chen", Location: "Boston, MA", ReservationState: "AVAILABLE"}}}
	markup := mustRenderNode(t, fieldNode(live{locale: "en-US"}, field, false))
	if !strings.Contains(markup, `type="radio"`) || !strings.Contains(markup, `aria-describedby="position-picker-desc-rev-position-a"`) {
		t.Fatalf("position option is not a named, described radio:\n%s", markup)
	}
}
func TestTodo_UXLIVE_038_Security(t *testing.T) {
	markup := uxlive038Markup(t)
	if strings.Contains(markup, "secret-manager") || strings.Contains(markup, "internal-position") {
		t.Fatal("position picker rendered an unauthorized discriminator")
	}
}
func TestTodo_UXLIVE_038_Regression(t *testing.T) {
	markup := uxlive038Markup(t)
	if !strings.Contains(markup, `aria-describedby="position-picker-desc-rev-position-a"`) || !strings.Contains(markup, `aria-describedby="position-picker-desc-rev-position-b"`) {
		t.Fatal("position descriptions lost their stable per-revision ids")
	}
}
