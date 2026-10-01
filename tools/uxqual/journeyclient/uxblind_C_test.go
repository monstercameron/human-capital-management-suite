package journeyclient

import (
	"strings"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

func TestTodo_UXBLIND_007(t *testing.T) {
	options := uxlive011Options(
		uxlive011Vacancy("ref-occupied", "PPL-DIR", "Director of People Operations"),
		uxlive011Vacancy("ref-open", "PPL-HRBP3", ""),
	)
	options.PromotionPaths = []*journeyv1.PromotionPathOption{{
		TargetJobCode: "PPL-HRBP3", TargetTitle: "Principal People Partner",
	}}
	field := uxlive011Field(t, ProposalForm(map[string]string{}, nil, "", options))
	if len(field.Vacancies) != 2 {
		t.Fatalf("picker options = %+v, want both published open projections", field.Vacancies)
	}
	if field.Vacancies[0].Title != "Director of People Operations" || field.Vacancies[1].Title != "Principal People Partner" {
		t.Fatalf("picker titles = %+v, want role titles rather than bare codes", field.Vacancies)
	}
	if field.Vacancies[0].Title == field.Vacancies[0].JobCode || field.Vacancies[1].Title == field.Vacancies[1].JobCode {
		t.Fatalf("a picker option still displays a bare job code: %+v", field.Vacancies)
	}
}

func TestTodo_UXBLIND_007_Browser(t *testing.T) {
	options := uxlive011Options(uxlive011Vacancy("ref-open", "PPL-HRBP3", "Principal People Partner"))
	field := uxlive011Field(t, ProposalForm(map[string]string{}, nil, "", options))
	for _, vacancy := range field.Vacancies {
		if strings.TrimSpace(vacancy.Title) == "" || vacancy.Title == vacancy.JobCode {
			t.Fatalf("browser option is not title-first: %+v", vacancy)
		}
	}
}

func TestTodo_UXBLIND_007_Property(t *testing.T) {
	options := uxlive011Options(
		uxlive011Vacancy("ref-a", "PPL-DIR", "Director of People Operations"),
		uxlive011Vacancy("ref-b", "PPL-HRBP3", "Principal People Partner"),
	)
	for _, vacancy := range options.GetPositionVacancies() {
		field := uxlive011Field(t, ProposalForm(map[string]string{FieldPosition: vacancy.GetReference()}, nil, "", options))
		if field.Value != vacancy.GetReference() {
			t.Fatalf("published open reference %q was not retained", vacancy.GetReference())
		}
	}
}

func TestTodo_UXBLIND_008(t *testing.T) {
	worker := &journeyv1.Worker{
		WorkerRef: "omar-reyes", JobCode: "OPS-HRBP2", Grade: "P2", BasePay: "135000.00", Currency: "USD",
	}
	values := map[string]string{FieldJobCode: "OPS-HRBP3", FieldGrade: "P3", FieldBase: "250000.00", FieldEffective: "2026-12-01", FieldReason: "Expanded scope"}
	page := ProposalPage(testConfig(), ListData{Workers: []*journeyv1.Worker{worker}, SelectedRef: worker.GetWorkerRef(), Options: testWorkforceOptions()}, nil, values)
	if page.Proposal == nil || len(page.Proposal.Form.Confirmation) != 0 {
		t.Fatalf("invalid proposal opened a review surface: %+v", page.Proposal.Form.Confirmation)
	}
	base, ok := fieldByID(page.Proposal.Form.Fields, FieldBase)
	if !ok || !strings.Contains(base.Error, "141,750") {
		t.Fatalf("out-of-range pay error = %q, want the published range", base.Error)
	}
}

func TestTodo_UXBLIND_009(t *testing.T) {
	options := testWorkforceOptions()
	before := focusedProposalForm(nil, "omar-reyes", options, findWorker(testWorkers(), "omar-reyes"))
	grade, _ := fieldByID(before.Fields, FieldGrade)
	if len(grade.Options) != 1 || grade.Options[0].Value != "" {
		t.Fatalf("grade options before role = %+v, want only the prompt", grade.Options)
	}
	after := focusedProposalForm(map[string]string{FieldJobCode: "OPS-HRBP3"}, "omar-reyes", options, findWorker(testWorkers(), "omar-reyes"))
	grade, _ = fieldByID(after.Fields, FieldGrade)
	if len(grade.Options) != 2 || grade.Options[1].Value != "P3" || !grade.Options[1].Selected {
		t.Fatalf("grade options after role = %+v, want selected P3 only", grade.Options)
	}
}

func TestTodo_UXBLIND_011(t *testing.T) {
	form := ProposalForm(map[string]string{}, testWorkers(), "omar-reyes", testWorkforceOptions())
	if form.Submit != productui.ResolveProductLocale("").Text("journey.form_submit") {
		t.Fatalf("proposal action = %q, want the review action", form.Submit)
	}
	actions := actionsLocale("en-US", journey.JourneyCard{IntentID: testIntentID, Stage: stageProposed}, "", nil, nil, nil, nil)
	if len(actions) == 0 || actions[0].Label != "Start approval workflow" || !strings.Contains(actions[0].Description, "not record") {
		t.Fatalf("saved proposal next step = %+v, want one explicit approval step", actions)
	}
}

func TestTodo_UXBLIND_012_Browser(t *testing.T) {
	values := map[string]string{FieldJobCode: "OPS-HRBP3", FieldGrade: "P3", FieldBase: "150000.00", FieldEffective: "2026-12-01", FieldReason: "Expanded scope"}
	form := focusedProposalForm(values, "omar-reyes", testWorkforceOptions(), findWorker(testWorkers(), "omar-reyes"))
	position, ok := fieldByID(form.Fields, FieldPosition)
	if !ok {
		t.Fatal("proposal form has no target-position field")
	}
	if position.Value != "" {
		t.Fatalf("unchosen target position was auto-selected as %q", position.Value)
	}
}

func TestTodo_UXBLIND_035(t *testing.T) {
	j := testJourney(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED)
	filter := listFilterView(productui.ResolveProductLocale(""), ListData{Journeys: []*journeyv1.Journey{j}}, 1, 1)
	if filter.Total != 1 || filter.ToggleLabel == "" {
		t.Fatalf("single-request filter = %+v, want a Filters control", filter)
	}
	if !journeyMatchesListFilter(j, productui.JourneyListFilter{Query: j.GetCorrelationId()}) {
		t.Fatalf("search did not accept the support reference %q", j.GetCorrelationId())
	}
}
