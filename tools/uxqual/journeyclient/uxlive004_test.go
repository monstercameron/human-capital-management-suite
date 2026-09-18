package journeyclient

import (
	"strings"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

// UXLIVE-004's RED was measured on the running server: "Confirm edit
// proposal" opened with target_job_code holding the value
// "WRK-CO2 · P2 → WRK-MGR · M2" and proposed_base holding
// "USD 68,000.00 → 98,000.00 (+44.1%)" -- the card's display strings, not
// values -- while the date field, handed a formatted date a date input
// cannot parse, rendered empty.
//
// Each field is now prefilled with its own current value in its own type,
// or left empty when the journey does not carry one.

func uxlive004Card(t *testing.T) journey.JourneyCard {
	t.Helper()
	j := &journeyv1.Journey{
		IntentId:      "01a0b18f-94ce-7560-b01f-71a6af3b581b",
		Stage:         journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL,
		Current:       &journeyv1.Placement{JobCode: "WRK-CO2", Grade: "P2"},
		Target:        &journeyv1.Placement{JobCode: "WRK-MGR", Grade: "M2"},
		Currency:      "USD",
		CurrentBase:   "68000.00",
		ProposedBase:  "98000.00",
		EffectiveDate: "2026-07-01",
	}
	return card(testConfig(), j)
}

func uxlive004EditFields(t *testing.T) map[string]journey.Field {
	t.Helper()
	head := uxlive004Card(t)
	for _, action := range interventionActions(nil, head, nil, nil, nil) {
		if action.ID != ActionEditProposal {
			continue
		}
		out := map[string]journey.Field{}
		for _, field := range action.Fields {
			out[field.Name] = field
		}
		return out
	}
	t.Fatalf("no edit-proposal action was offered")
	return nil
}

// TestTodo_UXLIVE_004 is the primary red/green test: every prefilled value is
// the field's own value, not a sentence about it.
func TestTodo_UXLIVE_004(t *testing.T) {
	fields := uxlive004EditFields(t)
	head := uxlive004Card(t)

	job := fields[NameEditJobCode]
	if job.Value == head.Headline || strings.ContainsAny(job.Value, "→·") {
		t.Fatalf("target job code is prefilled with a display string: %q", job.Value)
	}
	if job.Value != "WRK-MGR" {
		t.Fatalf("target job code = %q, want the proposal's own target %q", job.Value, "WRK-MGR")
	}

	grade := fields[NameEditGrade]
	if grade.Value != "M2" {
		t.Fatalf("target grade = %q, want %q", grade.Value, "M2")
	}

	base := fields[NameEditBase]
	if base.Value == head.PayLine || strings.ContainsAny(base.Value, "→()") {
		t.Fatalf("proposed base pay is prefilled with a display string: %q", base.Value)
	}
	if base.Value != "98000.00" {
		t.Fatalf("proposed base pay = %q, want the decimal the proposal carries %q", base.Value, "98000.00")
	}

	effective := fields[NameEditEffective]
	if effective.Value != "2026-07-01" {
		t.Fatalf("effective date = %q, want the value a date input accepts %q", effective.Value, "2026-07-01")
	}
}

// TestTodo_UXLIVE_004_Golden pins what an absent value does: the field is
// empty rather than carrying a placeholder that would be submitted.
func TestTodo_UXLIVE_004_Golden(t *testing.T) {
	head := card(testConfig(), &journeyv1.Journey{
		IntentId: "01a0b18f-94ce-7560-b01f-71a6af3b581b",
		Stage:    journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL,
	})
	for _, action := range interventionActions(nil, head, nil, nil, nil) {
		if action.ID != ActionEditProposal {
			continue
		}
		for _, field := range action.Fields {
			if strings.TrimSpace(field.Value) != "" {
				t.Fatalf("field %q carries %q for a journey that names no such value", field.Name, field.Value)
			}
		}
		return
	}
	t.Fatalf("no edit-proposal action was offered")
}

// TestTodo_UXLIVE_004_Security keeps the edit form's prefill inside what the
// page already shows: it carries no identifier or reference the card does
// not already carry.
func TestTodo_UXLIVE_004_Security(t *testing.T) {
	fields := uxlive004EditFields(t)
	for name, field := range fields {
		for _, forbidden := range []string{"eref:", "rev:", "ZXJlZjp2MT"} {
			if strings.Contains(field.Value, forbidden) {
				t.Fatalf("field %q leaks %q: %q", name, forbidden, field.Value)
			}
		}
	}
}
