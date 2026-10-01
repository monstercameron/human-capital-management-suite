package journey

import (
	"strings"
	"testing"
)

func uxblindCFormFields(base, role, grade, effective, reason string) []Field {
	return []Field{
		{ID: "propose-job", Name: "target_job_code", Kind: fieldKindSelect, Required: true, Value: role, Options: []Option{{Value: "", Label: "Select"}, {Value: role, Label: role, Selected: role != ""}}},
		{ID: "propose-grade", Name: "target_grade", Kind: fieldKindSelect, Required: true, Value: grade, Options: []Option{{Value: "", Label: "Select"}, {Value: grade, Label: grade, Selected: grade != ""}}},
		{ID: "propose-base", Name: "proposed_base", Kind: fieldKindNumber, Required: true, Value: base},
		{ID: "propose-effective", Name: "effective_date", Kind: fieldKindDate, Required: true, Value: effective},
		{ID: "propose-reason", Name: "business_reason", Kind: fieldKindTextarea, Required: true, Value: reason},
	}
}

func TestTodo_UXBLIND_008_Browser(t *testing.T) {
	form := ProposalForm{
		Submit: "Review and submit", Fields: uxblindCFormFields("", "", "", "", ""),
		Confirmation: []Fact{{Label: "Placement", Value: "PPL-HRBP3 · P3"}}, ConfirmationNote: "Review before saving.",
	}
	markup := mustRenderNode(t, proposalFormSection(live{locale: "en-US"}, form, "Promotion details"))
	if strings.Contains(markup, "review-surface") || strings.Contains(markup, "Check the proposal") {
		t.Fatalf("invalid proposal opened a review surface:\n%s", markup)
	}
	if !strings.Contains(markup, ">Review and submit<") {
		t.Fatalf("invalid proposal lost its direct save action:\n%s", markup)
	}
}

func TestTodo_UXBLIND_008_Accessibility(t *testing.T) {
	field := Field{ID: "propose-base", Name: "proposed_base", Kind: fieldKindNumber, Label: "Proposed base pay", Error: "Enter an amount within the published range."}
	markup := mustRenderNode(t, fieldNode(live{locale: "en-US"}, field, false))
	if !strings.Contains(markup, `aria-invalid="true"`) || !strings.Contains(markup, "Enter an amount within the published range.") {
		t.Fatalf("invalid pay field is not announced inline:\n%s", markup)
	}
}

func TestTodo_UXBLIND_009_Browser(t *testing.T) {
	before := Field{ID: "propose-grade", Name: "target_grade", Kind: fieldKindSelect, Label: "Target grade", Options: []Option{{Value: "", Label: "Select a target grade"}}}
	markup := mustRenderNode(t, fieldNode(live{locale: "en-US"}, before, false))
	if !strings.Contains(markup, `id="propose-grade"`) || !strings.Contains(markup, "disabled") {
		t.Fatalf("grade control was not disabled before a role was selected:\n%s", markup)
	}
	after := before
	after.Value = "P3"
	after.Options = []Option{{Value: "", Label: "Select a target grade"}, {Value: "P3", Label: "P3", Selected: true}}
	markup = mustRenderNode(t, fieldNode(live{locale: "en-US"}, after, false))
	if strings.Contains(markup, `id="propose-grade" disabled`) {
		t.Fatalf("grade control stayed disabled after a role was selected:\n%s", markup)
	}
}

func TestTodo_UXBLIND_011_Browser(t *testing.T) {
	form := ProposalForm{
		Submit: "Review and submit", Fields: uxblindCFormFields("150000.00", "PPL-HRBP3", "P3", "2026-12-01", "Expanded scope"),
		Confirmation: []Fact{{Label: "Placement", Value: "PPL-HRBP3 · P3"}}, ConfirmationNote: "Approvers have not been notified yet.",
	}
	markup := mustRenderNode(t, proposalFormSection(live{locale: "en-US"}, form, "Promotion details"))
	if !strings.Contains(markup, "Review and submit") || !strings.Contains(markup, ">Save proposal<") {
		t.Fatalf("saved-draft flow is not named consistently:\n%s", markup)
	}
}

func TestTodo_UXBLIND_035_Browser(t *testing.T) {
	filter := &JourneyFilterView{
		Label: "Find requests", ToggleLabel: "Filters", Total: 1, Result: "1 request",
		Fields: []Field{{ID: "journey-filter-q", Name: "q", Kind: fieldKindText, Label: "Search"}, {ID: "journey-filter-status", Name: "status", Kind: fieldKindSelect, Label: "Status"}},
	}
	markup := mustRenderNode(t, journeyFilterForm(live{locale: "en-US"}, filter))
	if !strings.Contains(markup, "Filters") || !strings.Contains(markup, "journey-filter-status") {
		t.Fatalf("single-request filter lost its accessible secondary-filter control:\n%s", markup)
	}
}
