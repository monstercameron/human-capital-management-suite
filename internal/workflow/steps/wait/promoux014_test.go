package wait_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/wait"
)

func TestTodo_PROMOUX_014_BoundedLocalDevAdvanceAndEffectiveDateExplanation(t *testing.T) {
	fireAt := mustInstant(t, "2026-06-17T14:00:00Z")
	requirement, err := wait.ComputeTimerRequirement(fixedInstantNode(t, fireAt), testDataset)
	if err != nil {
		t.Fatalf("ComputeTimerRequirement: %v", err)
	}

	explanation, err := wait.ExplainEffectiveDateWait(requirement, "promotion owner", "revalidate and commit promotion", "approvals and references", "notify owner when effective", "cancel with an authorized reason")
	if err != nil {
		t.Fatalf("ExplainEffectiveDateWait: %v", err)
	}
	if !explanation.EffectiveInstant.IsSet() || explanation.EffectiveInstant.String() != fireAt.String() {
		t.Fatalf("effective instant = %v, want %v", explanation.EffectiveInstant, fireAt)
	}
	if explanation.Timezone != testZone.String() || explanation.Owner == "" || explanation.ScheduledAction == "" || explanation.RemainingChecks == "" || explanation.NotificationBehavior == "" || explanation.AuthorizedIntervention == "" {
		t.Fatalf("incomplete wait explanation: %+v", explanation)
	}

	now := mustInstant(t, "2026-06-17T13:59:00Z")
	advanced, err := wait.Advance(wait.AdvanceRequest{Profile: wait.ProfileLocalDev, Requirement: requirement, Now: now, Target: fireAt})
	if err != nil || advanced.Outcome != wait.OutcomeFired {
		t.Fatalf("local-dev Advance = %+v, %v; want FIRED", advanced, err)
	}

	production, err := wait.Advance(wait.AdvanceRequest{Profile: wait.ProfileProduction, Requirement: requirement, Now: now, Target: fireAt})
	if err == nil || production != (wait.Resolution{}) {
		t.Fatalf("production Advance = %+v, %v; want a refused zero resolution", production, err)
	}

	tooFar := mustInstant(t, "2026-06-18T14:00:01Z")
	if _, err := wait.Advance(wait.AdvanceRequest{Profile: wait.ProfileLocalDev, Requirement: requirement, Now: now, Target: tooFar}); err == nil {
		t.Fatal("local-dev Advance accepted a target beyond its bound")
	}
	if _, err := wait.Advance(wait.AdvanceRequest{Profile: wait.ProfileLocalDev, Requirement: requirement, Now: values.Instant{}, Target: fireAt}); !errors.Is(err, wait.ErrAdvanceInstantsRequired) {
		t.Fatalf("missing-clock Advance error = %v, want ErrAdvanceInstantsRequired", err)
	}
	if _, err := wait.Advance(wait.AdvanceRequest{Profile: wait.ProfileLocalDev, Requirement: requirement, Now: fireAt, Target: now}); !errors.Is(err, wait.ErrAdvanceBackwards) {
		t.Fatalf("backwards Advance error = %v, want ErrAdvanceBackwards", err)
	}
}

func TestTodo_PROMOUX_014_ProductionAndUnknownProfilesFailClosed(t *testing.T) {
	production, err := wait.CapabilitiesFor(wait.ProfileProduction)
	if err != nil {
		t.Fatalf("production capabilities: %v", err)
	}
	if production.CanAdvanceEffectiveDate || production.MaxAdvanceSeconds != 0 {
		t.Fatalf("production capabilities = %+v, want no advance capability", production)
	}
	if _, err := wait.CapabilitiesFor("staging"); err == nil {
		t.Fatal("unknown profile received capabilities")
	}
}
