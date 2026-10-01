package journeyclient

import (
	"strings"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

func TestJourneyCapabilityAvailability(t *testing.T) {
	cfg := Config{PagePermissions: []PagePermission{
		{PageID: "chat", View: true},
		{PageID: "projects", View: false},
	}}
	got := JourneyCapabilityAvailability(cfg)
	if got == nil || !got.Chat || got.Projects {
		t.Fatalf("capability projection = %+v, want chat available and projects unavailable", got)
	}
	if got := JourneyCapabilityAvailability(Config{}); got != nil {
		t.Fatalf("empty permission map should preserve legacy signal absence, got %+v", got)
	}
}

func uxblindRPromotionOptions() *journeyv1.WorkforceOptions {
	return &journeyv1.WorkforceOptions{
		Currency: "USD",
		PromotionPaths: []*journeyv1.PromotionPathOption{{
			SourceJobCode: "OPS-HRBP2", SourceGrade: "P2",
			TargetJobCode: "OPS-HRBP3", TargetGrade: "P3",
			MinimumBaseIncrease: "0.0500", MaximumBaseIncrease: "0.5000",
		}},
	}
}

func uxblindRActionField(t *testing.T, action journey.Action, id string) journey.Field {
	t.Helper()
	for _, field := range action.Fields {
		if field.ID == id {
			return field
		}
	}
	t.Fatalf("action %q has no field %q: %+v", action.ID, id, action.Fields)
	return journey.Field{}
}

func uxblindRPageMarkup(t *testing.T, action journey.Action) string {
	t.Helper()
	markup, err := journey.RenderToString(journey.Page{
		Locale: "en-US",
		Detail: &journey.DetailView{
			Journey: journey.JourneyCard{WorkerName: "Ana Silva", StageLabel: "Awaiting approval"},
			Actions: []journey.Action{action},
		},
	})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	return markup
}

func uxblindRInterventionActions(t *testing.T, options *journeyv1.WorkforceOptions) []journey.Action {
	t.Helper()
	head := card(testConfig(), testJourney(t, journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL))
	return interventionActionsLocale("en-US", nil, head, nil, nil, nil, options)
}

func TestTodo_UXBLIND_059(t *testing.T) {
	actions := uxblindRInterventionActions(t, uxblindRPromotionOptions())
	edit := findAction(t, actions, ActionEditProposal)
	if edit.ConfirmTitle != "Edit proposal" {
		t.Fatalf("edit title = %q, want an edit title", edit.ConfirmTitle)
	}
	job := uxblindRActionField(t, edit, FieldEditJobCode)
	grade := uxblindRActionField(t, edit, FieldEditGrade)
	base := uxblindRActionField(t, edit, FieldEditBase)
	reason := uxblindRActionField(t, edit, FieldEditBusinessReason)
	if job.Kind != kindSelect || grade.Kind != kindSelect {
		t.Fatalf("edit placement controls = %q/%q, want governed selects", job.Kind, grade.Kind)
	}
	if job.Value != "OPS-HRBP3" || grade.Value != "P3" || base.Value != "98000.00" {
		t.Fatalf("edit defaults lost proposal values: job=%q grade=%q base=%q", job.Value, grade.Value, base.Value)
	}
	if reason.Value != "Runs the EMEA HRBP portfolio single-handed." {
		t.Fatalf("business reason was not prefilled: %q", reason.Value)
	}
	if base.Prefix != "USD" || base.Suffix != "per year" || !strings.Contains(base.Help, "Allowed") {
		t.Fatalf("edit pay control lacks the new-promotion adornments/range hint: %+v", base)
	}
}

func TestTodo_UXBLIND_059_Browser(t *testing.T) {
	edit := findAction(t, uxblindRInterventionActions(t, uxblindRPromotionOptions()), ActionEditProposal)
	markup := uxblindRPageMarkup(t, edit)
	for _, want := range []string{`<select`, `name="target_job_code"`, `name="target_grade"`, `USD`, "per year", "Runs the EMEA HRBP portfolio single-handed.", "Edit proposal"} {
		if !strings.Contains(markup, want) {
			t.Errorf("rendered edit dialog lacks %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "Confirm edit") {
		t.Fatalf("rendered edit dialog still uses confirmation wording: %s", markup)
	}
}

func TestTodo_UXBLIND_059_Regression(t *testing.T) {
	edit := findAction(t, uxblindRInterventionActions(t, nil), ActionEditProposal)
	if uxblindRActionField(t, edit, FieldEditJobCode).Kind != kindSelect || uxblindRActionField(t, edit, FieldEditGrade).Kind != kindSelect {
		t.Fatalf("edit regressed to free text without options: %+v", edit.Fields)
	}
	if got := uxblindRActionField(t, edit, FieldEditBusinessReason).Value; got == "" {
		t.Fatal("edit regression: business reason is empty")
	}
}

func TestTodo_UXBLIND_060(t *testing.T) {
	actions := uxblindRInterventionActions(t, uxblindRPromotionOptions())
	edit := findAction(t, actions, ActionEditProposal)
	edit.Fields[0].Error = "Fill in this field."
	markup := uxblindRPageMarkup(t, edit)
	if strings.Contains(markup, `class="jn-noticeband"`) {
		t.Fatalf("dialog validation escaped into a page banner: %s", markup)
	}
	if !strings.Contains(markup, "Fill in this field.") {
		t.Fatalf("dialog field error was not rendered in the dialog: %s", markup)
	}

	a := &App{store: journey.NewStore(journey.Page{Detail: &journey.DetailView{Actions: []journey.Action{edit}}}), editErrors: map[string]string{FieldEditJobCode: "Fill in this field."}}
	a.dismissReview(ActionEditProposal)
	if len(a.editErrors) != 0 {
		t.Fatalf("dismissed edit review retained validation state: %+v", a.editErrors)
	}
	for _, field := range a.store.Page().Detail.Actions[0].Fields {
		if field.Error != "" {
			t.Fatalf("dismissed edit review retained field error: %+v", field)
		}
	}
}

func TestTodo_UXBLIND_060_Browser(t *testing.T) {
	action := findAction(t, uxblindRInterventionActions(t, uxblindRPromotionOptions()), ActionEditProposal)
	action.Fields[0].Error = "Fill in this field."
	markup := uxblindRPageMarkup(t, action)
	if strings.Contains(markup, `class="jn-noticeband"`) || !strings.Contains(markup, "Fill in this field.") {
		t.Fatalf("dialog error is not scoped to the rendered dialog: %s", markup)
	}
}

func TestTodo_UXBLIND_061(t *testing.T) {
	cancel := findAction(t, uxblindRInterventionActions(t, nil), ActionCancel)
	if cancel.Variant != "danger" {
		t.Fatalf("cancel action variant = %q, want danger", cancel.Variant)
	}
	if cancel.CancelLabel != "Keep request" {
		t.Fatalf("cancellation dismiss label = %q, want Keep request", cancel.CancelLabel)
	}
	if strings.Contains(cancel.Description, "safe point") || !strings.Contains(uxblindRActionField(t, cancel, FieldCancelReason).Help, "Retained as evidence") {
		t.Fatalf("cancellation copy still exposes engine terminology: %+v", cancel)
	}
}

func TestTodo_UXBLIND_061_Browser(t *testing.T) {
	cancel := findAction(t, uxblindRInterventionActions(t, nil), ActionCancel)
	markup := uxblindRPageMarkup(t, cancel)
	if !strings.Contains(markup, `data-variant="danger"`) || !strings.Contains(markup, "Keep request") || !strings.Contains(markup, "Request cancellation") {
		t.Fatalf("destructive cancellation dialog is ambiguous or not styled: %s", markup)
	}
}
