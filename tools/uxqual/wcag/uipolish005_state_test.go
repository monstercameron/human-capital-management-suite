package wcag

import (
	"strings"
	"testing"
)

func TestTodo_UIPOLISH_005_StateQualification(t *testing.T) {
	results := QualifyInteractionStates([]InteractionState{
		{Name: "focus", Foreground: "#1d4f91", Background: "#ffffff", MinRatio: 3, Cue: "2px outline"},
		{Name: "selected", Foreground: "#1a1d29", Background: "#dbeafe", Cue: "checkmark"},
	})
	for _, result := range results {
		if !result.Pass {
			t.Fatalf("state %q failed: %s", result.State.Name, result.Error)
		}
	}
}

func TestTodo_UIPOLISH_005_StateQualificationRejectsColourOnlyAndLowContrast(t *testing.T) {
	results := QualifyInteractionStates([]InteractionState{
		{Name: "hover", Foreground: "#ffffff", Background: "#ffffff", Cue: "underline"},
		{Name: "visited", Foreground: "#1a1d29", Background: "#ffffff"},
		{Name: "active", Foreground: "not-a-colour", Background: "#ffffff", Cue: "pressed"},
	})
	if results[0].Pass || !strings.Contains(results[0].Error, "below") {
		t.Fatalf("low-contrast state result = %+v", results[0])
	}
	if results[1].Pass || !strings.Contains(results[1].Error, "non-colour") {
		t.Fatalf("colour-only state result = %+v", results[1])
	}
	if results[2].Pass || !strings.Contains(results[2].Error, "expected #rrggbb") {
		t.Fatalf("malformed state result = %+v", results[2])
	}
}

func TestTodo_UIPOLISH_005_StateQualificationRejectsEmptyAndWeakEssentialFloor(t *testing.T) {
	if results := QualifyInteractionStates(nil); len(results) != 1 || results[0].Pass || !strings.Contains(results[0].Error, "at least one") {
		t.Fatalf("empty state result = %+v", results)
	}
	results := QualifyInteractionStates([]InteractionState{{
		Name: "control-border", Foreground: "#1a1d29", Background: "#ffffff", MinRatio: 1.1, Cue: "outline", Essential: true,
	}})
	if results[0].Pass || !strings.Contains(results[0].Error, "essential state floor") {
		t.Fatalf("weak essential floor result = %+v", results[0])
	}
}

func TestTodo_UIPOLISH_005_StatusSemanticsRejectColourOnly(t *testing.T) {
	errs := QualifyStatusSemantics([]StatusSemantic{
		{Name: "ready", Label: "Ready", Cue: "check icon"},
		{Name: "failed", Label: "Failed"},
		{Name: "pending", Cue: "spinner"},
	})
	if len(errs) != 2 {
		t.Fatalf("status errors = %v, want two failures", errs)
	}
	if !strings.Contains(errs[0].Error(), "colour alone") || !strings.Contains(errs[1].Error(), "text") {
		t.Fatalf("status errors lack actionable reasons: %v", errs)
	}
}
