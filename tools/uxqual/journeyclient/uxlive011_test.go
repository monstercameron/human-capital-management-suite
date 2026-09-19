package journeyclient

import (
	"strings"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

// UXLIVE-011's RED was read off the served form: "Promote Amara" rendered
// "Target position (optional)" as a free-text input whose own help said
// "Entering an unconfirmed position will block the request." The refusal was
// real -- promotion.evaluateTargetPositionSelection has required a
// server-issued position.RevisionRef since PROMOUX-004 -- so the box could
// only ever produce a blocked proposal. A field that predicts its own
// refusal is a defect, not a warning.
//
// The fix is the list, not the control: PROMOUX-004 already built the filter
// and the accessible picker, and neither could be used because nothing
// published the authorized vacancies. Now the cell does, and this form
// offers them and nothing else.

func uxlive011Options(vacancies ...*journeyv1.PositionVacancyOption) *journeyv1.WorkforceOptions {
	return &journeyv1.WorkforceOptions{PositionVacancies: vacancies}
}

func uxlive011Vacancy(reference, jobCode, title string) *journeyv1.PositionVacancyOption {
	return &journeyv1.PositionVacancyOption{
		Reference: reference, JobCode: jobCode, Title: title,
		Organization: "Care Coordination", Location: "Boston, MA",
		ReservationState: "AVAILABLE",
	}
}

func uxlive011Field(t *testing.T, form journey.ProposalForm) journey.Field {
	t.Helper()
	for _, field := range form.Fields {
		if field.ID == FieldPosition {
			return field
		}
	}
	t.Fatalf("the proposal form has no target-position field at all")
	return journey.Field{}
}

// TestTodo_UXLIVE_011 is the primary red/green test: the target position is
// a governed choice over published vacancies, and there is no text path.
func TestTodo_UXLIVE_011(t *testing.T) {
	options := uxlive011Options(
		uxlive011Vacancy("ref-eng-1", "ENG-MGR", "Engineering Manager"),
		uxlive011Vacancy("ref-care-1", "CARE-COORD", "Care Coordinator"),
	)

	field := uxlive011Field(t, ProposalForm(map[string]string{}, nil, "", options))
	if field.Kind == kindText {
		t.Fatalf("the target position is still a free-text input")
	}
	if field.Kind != kindPositionPicker {
		t.Fatalf("the target position is a %q control, want the governed picker", field.Kind)
	}
	if len(field.Vacancies) != 2 {
		t.Fatalf("the picker offers %d of the 2 published vacancies", len(field.Vacancies))
	}
	if field.Vacancies[0].Reference != "ref-eng-1" || field.Vacancies[0].Title != "Engineering Manager" {
		t.Fatalf("the first option is %+v, want the published vacancy verbatim", field.Vacancies[0])
	}

	// Choosing a role narrows the list to what is open for it.
	narrowed := uxlive011Field(t, ProposalForm(map[string]string{FieldJobCode: "CARE-COORD"}, nil, "", options))
	if len(narrowed.Vacancies) != 1 || narrowed.Vacancies[0].Reference != "ref-care-1" {
		t.Fatalf("choosing CARE-COORD offers %+v, want only the open CARE-COORD position", narrowed.Vacancies)
	}

	// A role with nothing open is an answer, not a prompt to type one in.
	empty := uxlive011Field(t, ProposalForm(map[string]string{FieldJobCode: "FIN-DIR"}, nil, "", options))
	if len(empty.Vacancies) != 0 {
		t.Fatalf("a role with no open position offered %+v", empty.Vacancies)
	}
	if empty.Kind != kindPositionPicker {
		t.Fatalf("an empty picker fell back to a %q control", empty.Kind)
	}
	if strings.TrimSpace(empty.EmptyTitle) == "" || strings.TrimSpace(empty.EmptyDetail) == "" {
		t.Fatalf("the no-vacancy state says nothing: title %q, detail %q", empty.EmptyTitle, empty.EmptyDetail)
	}
	if !strings.Contains(empty.EmptyDetail, "propose") {
		t.Fatalf("the no-vacancy state does not explain what proceeding without one means: %q", empty.EmptyDetail)
	}
}

// TestTodo_UXLIVE_011_Browser keeps the help honest. The old copy told the
// reader their entry would block the request; copy that predicts a refusal
// belongs to a control that can produce one, and this one cannot.
func TestTodo_UXLIVE_011_Browser(t *testing.T) {
	field := uxlive011Field(t, ProposalForm(map[string]string{}, nil, "",
		uxlive011Options(uxlive011Vacancy("ref-1", "ENG-MGR", "Engineering Manager"))))

	if strings.Contains(strings.ToLower(field.Help), "block the request") {
		t.Fatalf("the help still predicts the field's own refusal: %q", field.Help)
	}
	if strings.TrimSpace(field.Help) == "" {
		t.Fatalf("the picker carries no help at all")
	}
	if field.Placeholder != "" {
		t.Fatalf("the picker carries a text placeholder: %q", field.Placeholder)
	}
	if field.Label == "" {
		t.Fatalf("the picker has no legend to name the group")
	}

	// Every locale answers, so the picker is never rendered with a raw key
	// or an empty legend in a language the audit did not run in.
	for _, locale := range []string{"en-US", "de-DE", "ar-EG"} {
		form := ProposalForm(map[string]string{}, nil, "", uxlive011Options())
		localizeProposalForm(&form, productui.ResolveProductLocale(locale), false, nil, nil)
		localized := uxlive011Field(t, form)
		for name, text := range map[string]string{
			"label": localized.Label, "help": localized.Help,
			"empty title": localized.EmptyTitle, "empty detail": localized.EmptyDetail,
		} {
			if strings.TrimSpace(text) == "" || strings.HasPrefix(text, "journey.") {
				t.Errorf("%s: the picker's %s is unlocalized: %q", locale, name, text)
			}
		}
	}
}

// TestTodo_UXLIVE_011_Security is the no-free-text guarantee stated as a
// property: nothing the browser holds can put a position on a proposal
// unless the cell published it.
func TestTodo_UXLIVE_011_Security(t *testing.T) {
	options := uxlive011Options(uxlive011Vacancy("ref-published", "ENG-MGR", "Engineering Manager"))

	// A value the server never published is dropped, not carried.
	for _, guessed := range []string{"POS-ENG-MGR-101", "ref-published-x", " ref-published", "'; drop"} {
		field := uxlive011Field(t, ProposalForm(map[string]string{FieldPosition: guessed}, nil, "", options))
		if field.Value != "" {
			t.Errorf("a guessed position %q survived as the field's value %q", guessed, field.Value)
		}
	}

	// A published one is kept, so this is a filter and not a blanket clear.
	kept := uxlive011Field(t, ProposalForm(map[string]string{FieldPosition: "ref-published"}, nil, "", options))
	if kept.Value != "ref-published" {
		t.Fatalf("a published reference was dropped: %q", kept.Value)
	}

	// A previously chosen position that is no longer offered -- filled since
	// the form was drawn -- is dropped rather than resubmitted, because the
	// proposal would be refused on exactly that ground.
	stale := uxlive011Field(t, ProposalForm(map[string]string{FieldPosition: "ref-published", FieldJobCode: "FIN-DIR"}, nil, "", options))
	if stale.Value != "" {
		t.Fatalf("a position no longer on offer was still carried: %q", stale.Value)
	}

	// A published option with no reference is not offered at all: an option
	// whose value is empty would submit as "no position chosen" while
	// looking like a choice.
	blank := uxlive011Field(t, ProposalForm(map[string]string{}, nil, "",
		uxlive011Options(&journeyv1.PositionVacancyOption{Title: "Unnamed", JobCode: "ENG-MGR"})))
	if len(blank.Vacancies) != 0 {
		t.Fatalf("an option with no server-issued reference was offered: %+v", blank.Vacancies)
	}
}

// TestPositionPickerStartsWithTheEmployeesNextRoles: before a role is chosen
// the person-scoped form listed every open position in the tenant, although
// only positions for the employee's published next roles can be accepted.
func TestPositionPickerStartsWithTheEmployeesNextRoles(t *testing.T) {
	options := testWorkforceOptions()
	options.PositionVacancies = []*journeyv1.PositionVacancyOption{
		uxlive011Vacancy("ref-mgr", "ENG-MGR1", "Engineering Manager"),
		uxlive011Vacancy("ref-nurse", "CLN-NURSE4", "Charge Nurse"),
		uxlive011Vacancy("ref-open", "", "Unassigned role"),
	}
	jane := findWorker(testWorkers(), "jane-doe")
	if jane == nil {
		t.Fatal("fixture has no jane-doe")
	}
	got := map[string]bool{}
	for _, vacancy := range uxlive011Field(t, focusedProposalForm(map[string]string{}, "jane-doe", options, jane)).Vacancies {
		got[vacancy.Reference] = true
	}
	if !got["ref-mgr"] || !got["ref-open"] || got["ref-nurse"] {
		t.Fatalf("vacancies before a role is chosen = %v, want the ENG-MGR1 and role-less positions only", got)
	}
}

// TestProposalReviewIsLocalizedAndExact: the review dialog printed English
// labels and en-US money on a German page, and rounded an over-precise entry
// so the reader confirmed an amount other than the one typed.
func TestProposalReviewIsLocalizedAndExact(t *testing.T) {
	jane := findWorker(testWorkers(), "jane-doe")
	facts := proposalConfirmationLocale("de-DE", jane, testWorkforceOptions(), "ENG-MGR1", "M1", "150000.50", "2026-12-01")
	copy := productui.ResolveProductLocale("de-DE")
	labels := map[string]string{}
	for _, fact := range facts {
		labels[fact.Label] = fact.Value
	}
	pay, ok := labels[copy.Text("journey.action_base")]
	if !ok || !strings.Contains(pay, "150.000,50") {
		t.Fatalf("German review facts = %+v, want localized labels and 150.000,50", facts)
	}
	for _, fact := range proposalConfirmationLocale("en-US", jane, testWorkforceOptions(), "ENG-MGR1", "M1", "150000.555", "2026-12-01") {
		if fact.Label == "Base pay" && !strings.Contains(fact.Value, "150000.555") {
			t.Fatalf("over-precise entry shown as %q, want it exactly as typed", fact.Value)
		}
	}
}
