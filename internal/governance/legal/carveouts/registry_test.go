package carveouts

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func loadFixture(t *testing.T) Bundle {
	t.Helper()
	b, err := LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	return b
}

func TestTodo_LEGAL_TOOL_007(t *testing.T) {
	b := loadFixture(t)
	ca, ok := b.Carveouts.State("CA")
	if !ok || !ca.SalaryHistoryBan || ca.PostingEmployerSizeFloor != 15 {
		t.Fatalf("California carveout = %+v, want a size-independent ban and 15-employee posting floor", ca)
	}
	if !ca.Allows(SalaryHistoryBan, VoluntaryDisclosure, 1) {
		t.Fatal("California voluntary disclosure must be evaluated as a pre-trigger exception")
	}
	if ca.PostingRequired(14) || !ca.PostingRequired(15) {
		t.Fatal("California posting threshold did not preserve below/at-threshold behavior")
	}
	de, _ := b.Carveouts.State("DE")
	if !de.Allows(SalaryHistoryBan, VoluntaryDisclosure, 1) {
		t.Fatal("Delaware voluntary disclosure carveout missing")
	}
	hi, _ := b.Carveouts.State("HI")
	if !hi.SalaryHistoryBan || !hi.Allows(PostingMandate, EmployerSizeBelowThreshold, 49) || hi.PostingRequired(49) {
		t.Fatal("Hawaii ban/posting size carveout is not distinct")
	}
}

func TestTodo_LEGAL_TOOL_007_Golden(t *testing.T) {
	b := loadFixture(t)
	want := map[string]struct {
		ban   bool
		floor int
	}{"CA": {true, 15}, "DE": {true, 0}, "HI": {true, 50}}
	for state, expected := range want {
		row, ok := b.Carveouts.State(state)
		if !ok || row.SalaryHistoryBan != expected.ban || row.PostingEmployerSizeFloor != expected.floor {
			t.Fatalf("golden %s = %+v, want ban=%t floor=%d", state, row, expected.ban, expected.floor)
		}
	}
	if len(b.Carveouts.States) != 51 || b.Carveouts.Digest() == "" {
		t.Fatalf("carveout golden metadata = rows %d digest %q", len(b.Carveouts.States), b.Carveouts.Digest())
	}
}

func TestTodo_LEGAL_TOOL_007_Conformance(t *testing.T) {
	b := loadFixture(t)
	for _, row := range b.Carveouts.States {
		if row.Citation.Review != ReviewReviewed || !strings.HasPrefix(row.Citation.SourceFile, "planning/research/state-employment-law/") {
			t.Fatalf("%s has incomplete citation/review: %+v", row.State, row.Citation)
		}
		for i, ex := range row.Exceptions {
			if err := ex.validate("exception"); err != nil {
				t.Fatalf("%s exception %d: %v", row.State, i, err)
			}
		}
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("bundle validation: %v", err)
	}
}

func TestTodo_LEGAL_TOOL_008(t *testing.T) {
	b := loadFixture(t)
	for _, row := range b.SeparationFiling.States {
		if row.FormName == "" || row.FormName == "?" || row.RecipientAuthority == "" || row.Citation.Section == "" {
			t.Fatalf("unresolved UI row: %+v", row)
		}
	}
	for _, state := range []string{"GA", "IN", "KY", "CO"} {
		row, ok := b.SeparationFiling.State(state)
		if !ok || row.FormName == "NONE_IDENTIFIED" || row.Citation.Review != ReviewReviewed {
			t.Fatalf("%s did not resolve to a reviewed named format: %+v", state, row)
		}
	}
}

func TestTodo_LEGAL_TOOL_008_Golden(t *testing.T) {
	b := loadFixture(t)
	for _, tc := range []struct{ state, form, authority string }{
		{"GA", "DOL-402A / DOL-800", "Georgia Department of Labor"},
		{"IN", "UI-14", "Indiana Department of Labor"},
		{"KY", "UK-ES", "Kentucky Department of Unemployment Insurance"},
		{"CO", "SB 22-234 separation notice", "Colorado Department of Labor and Employment"},
	} {
		row, ok := b.SeparationFiling.State(tc.state)
		if !ok || row.FormName != tc.form || row.RecipientAuthority != tc.authority {
			t.Fatalf("%s = %+v, want form %q authority %q", tc.state, row, tc.form, tc.authority)
		}
	}
}

func TestTodo_LEGAL_TOOL_008_Conformance(t *testing.T) {
	b := loadFixture(t)
	if len(b.SeparationFiling.States) != 51 || b.SeparationFiling.Digest() == "" {
		t.Fatalf("separation registry rows/digest = %d/%q", len(b.SeparationFiling.States), b.SeparationFiling.Digest())
	}
	for _, row := range b.SeparationFiling.States {
		if row.FormName == "NONE_IDENTIFIED" && row.Citation.Review != ReviewUnreviewed {
			t.Fatalf("unresolved form should remain visibly unreviewed: %s", row.State)
		}
	}
}

func TestTodo_LEGAL_TOOL_009(t *testing.T) {
	b := loadFixture(t)
	stateFloor, err := values.NewMoney("15.00", "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	got, err := b.LocalityOverlays.MinimumWage("IL", "Chicago", stateFloor)
	if err != nil {
		t.Fatalf("Chicago minimum wage: %v", err)
	}
	if got.String() != "16.60 USD" {
		t.Fatalf("Chicago minimum wage = %q, want 16.60 USD", got.String())
	}
	if _, err := b.LocalityOverlays.MinimumWage("IL", "Cook County", stateFloor); err != nil {
		t.Fatalf("Cook County state-floor fallback: %v", err)
	}
	milwaukee, ok := findOverlay(b.LocalityOverlays, "WI", "Milwaukee")
	if !ok || !milwaukee.Preempted {
		t.Fatalf("Milwaukee = %+v, want an explicit preempted row", milwaukee)
	}
	wi, ok := b.LocalityOverlays.State("WI")
	if !ok || !wi.Preempts("WAGE_FLOOR") || !wi.Preempts("LEAVE_INTERACTION") {
		t.Fatalf("Wisconsin preemption = %+v, want wage and leave kinds", wi)
	}
}

func TestTodo_LEGAL_TOOL_009_Golden(t *testing.T) {
	b := loadFixture(t)
	for _, tc := range []struct {
		state, locality string
		wage            string
		leave           int
	}{
		{"IL", "Chicago", "16.60 USD", 80},
		{"IL", "Cook County", "15.00 USD", 40},
		{"MD", "Montgomery County", "17.65 USD", 56},
	} {
		floor, err := values.NewMoney("15.00", "USD", 2, values.RoundingHalfEven)
		if err != nil {
			t.Fatal(err)
		}
		wage, err := b.LocalityOverlays.MinimumWage(tc.state, tc.locality, floor)
		if err != nil || wage.String() != tc.wage {
			t.Fatalf("%s/%s wage = %q, %v, want %q", tc.state, tc.locality, wage.String(), err, tc.wage)
		}
		leave, found, err := b.LocalityOverlays.PaidLeaveHours(tc.state, tc.locality)
		if err != nil || !found || leave != tc.leave {
			t.Fatalf("%s/%s leave = %d/%t/%v, want %d/true", tc.state, tc.locality, leave, found, err, tc.leave)
		}
	}
}

func TestTodo_LEGAL_TOOL_009_Conformance(t *testing.T) {
	b := loadFixture(t)
	if len(b.LocalityOverlays.States) != 51 || b.LocalityOverlays.Digest() == "" {
		t.Fatalf("locality registry rows/digest = %d/%q", len(b.LocalityOverlays.States), b.LocalityOverlays.Digest())
	}
	for _, row := range b.LocalityOverlays.States {
		if row.Citation.SourceFile == "" || row.Citation.Section == "" || !row.Citation.Review.valid() {
			t.Fatalf("state locality row lacks citation/review: %+v", row)
		}
	}
	for _, state := range []string{"FL", "NC"} {
		row, ok := findStateLocality(b.LocalityOverlays, state)
		if !ok || !row.NoLocalityFound {
			t.Fatalf("%s must have an explicit no-locality finding", state)
		}
	}
}

func TestTodo_LEGAL_TOOL_RegistriesRejectMissingField(t *testing.T) {
	b := loadFixture(t)
	b.Carveouts.States[0].Duty.Exceptions = []PayTransparencyException{{AppliesTo: SalaryHistoryBan, ExceptionKind: EmployerSizeBelowThreshold, Citation: b.Carveouts.States[0].Citation}}
	err := b.Carveouts.Validate()
	var fieldErr *ValidationError
	if !errors.As(err, &fieldErr) || fieldErr.Field != "states[0].duty.exceptions[0].employer_size_threshold" {
		t.Fatalf("error = %v, field = %+v, want a typed threshold refusal", err, fieldErr)
	}
}

func TestTodo_LEGAL_TOOL_RegistriesDigestAndExplain(t *testing.T) {
	first := loadFixture(t)
	second := loadFixture(t)
	if first.Carveouts.Digest() != second.Carveouts.Digest() || first.SeparationFiling.Digest() != second.SeparationFiling.Digest() || first.LocalityOverlays.Digest() != second.LocalityOverlays.Digest() {
		t.Fatal("fixture digests are not stable")
	}
	if !strings.Contains(first.Explain(), "no transmission") || !strings.Contains(Explain(), "effective-dated") {
		t.Fatalf("explain output does not describe the reference-only contract: %s", first.Explain())
	}
}

func findOverlay(r LocalityOverlayRegistry, state, locality string) (LocalityOverlay, bool) {
	for _, row := range r.Overlays {
		if row.State == state && row.Locality == locality {
			return row, true
		}
	}
	return LocalityOverlay{}, false
}

func findStateLocality(r LocalityOverlayRegistry, state string) (StateLocalityRule, bool) {
	for _, row := range r.States {
		if row.State == state {
			return row, true
		}
	}
	return StateLocalityRule{}, false
}
