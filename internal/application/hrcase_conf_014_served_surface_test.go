package application_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	hrcase "github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/hrcase"
)

// TestTodo_CONF_014_Served proves CONF-014's GREEN clause through the served
// application boundary: the HR case compartment fixture built only from the
// served surface compiles its reference plan for every scenario environment
// and exposes the same capability set as the conformance owner.
func TestTodo_CONF_014_Served(t *testing.T) {
	var nilApp *application.App
	if got := nilApp.HRCaseConformance().CapabilityIDs; got != nil {
		t.Fatalf("nil app surface exposes capabilities, want none")
	}

	surface := application.NewServedHRCaseConformanceSurface()
	want := hrcase.CapabilityIDs()
	gotIDs := surface.CapabilityIDs()
	if len(gotIDs) == 0 || len(gotIDs) != len(want) {
		t.Fatalf("served capability ids = %v, want %d ids", gotIDs, len(want))
	}
	for i, key := range want {
		if gotIDs[i] != key.ID {
			t.Fatalf("served capability[%d] = %q, want %q", i, gotIDs[i], key.ID)
		}
	}

	scenarios := map[string]func() *hrcase.Environment{
		"golden":       surface.GoldenEnvironment,
		"unauthorized": surface.UnauthorizedParticipantEnvironment,
		"matter_wall":  surface.MatterWallEnvironment,
		"legal_hold":   surface.LegalHoldEnvironment,
		"retaliation":  surface.RetaliationSignalEnvironment,
		"disposed":     surface.AlreadyDisposedEnvironment,
		"degraded":     surface.DegradedObservationEnvironment,
	}
	for name, env := range scenarios {
		setup, err := surface.NewSetup(env())
		if err != nil {
			t.Fatalf("served NewSetup(%s): %v", name, err)
		}
		if setup == nil || setup.Plan == nil || setup.Env == nil {
			t.Fatalf("served NewSetup(%s) returned an incomplete setup", name)
		}
	}

	// Investigator conflict is a RED scenario: the served setup must still
	// build so the simulation can refuse it, not fail at wiring time.
	conflict, err := surface.NewSetupWithParams(surface.GoldenEnvironment(), hrcase.Params{InvestigatorID: "subject-worker"})
	if err != nil || conflict == nil || conflict.Plan == nil {
		t.Fatalf("served NewSetupWithParams(investigator conflict) = %v, %v", conflict, err)
	}
}
