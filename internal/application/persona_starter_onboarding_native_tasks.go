package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/workerlifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var _ PersonaOnboardingTaskStatePort = (*NativePersonaOnboardingPort)(nil)

// TaskStates reports accepted child lifecycle state. An OBSERVED outcome is
// the owner's governed observation; human task completion cannot substitute
// for it. A T0 read never emits work or changes the retained tracker.
func (p *NativePersonaOnboardingPort) TaskStates(ctx context.Context, worker values.EntityRef) ([]PersonaOnboardingTaskState, error) {
	snapshot, _, err := p.current(ctx, worker)
	if err != nil {
		return nil, err
	}
	states := make([]PersonaOnboardingTaskState, 0, len(snapshot.Tracker.Children))
	for _, child := range snapshot.Tracker.Children {
		state := PersonaOnboardingTaskState{ChildID: child.ChildID, State: child.State}
		if child.State != workerlifecycle.StatusChildPending {
			state.Intent = &workerlifecycle.ChildIntent{ID: child.IntentID, ChildID: child.ChildID, IntentType: child.IntentType, IntentVersion: child.IntentVersion, ScopeDigest: child.ScopeDigest, PlanDigest: snapshot.Tracker.PlanDigest, Revision: snapshot.Tracker.Revision}
		}
		switch child.State {
		case workerlifecycle.StatusChildObserved:
			state.Outcome = workerlifecycle.ChildObserved
			state.ObservationRef = child.ObservationRef.String()
		case workerlifecycle.StatusChildFailed:
			state.Outcome = workerlifecycle.ChildFailed
			// The lifecycle owner records the failed outcome and repair obligation,
			// but its failed transition does not retain an observation reference.
		}
		states = append(states, state)
	}
	return states, nil
}
