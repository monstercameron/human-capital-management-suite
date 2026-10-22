package application

import (
	"errors"
	"time"
)

// personaRunFailurePersistTimeout bounds writing a run's terminal failure after
// the context the run worked under has ended.
const personaRunFailurePersistTimeout = 10 * time.Second

// errPersonaModelRouteUnavailable marks a run whose model route policy cannot
// be used right now: it is missing, past its effective window, or no longer
// satisfies the model contract. Somebody has to repair the agent's setup, which
// is a different message from "this agent cannot read that for you".
var errPersonaModelRouteUnavailable = errors.New("application: current tenant model route unavailable")

// personaRunModelWorkFailureCode is the terminal code of a run whose model work
// could not be built. A route policy that is stale or invalid ends the run as
// MODEL_ROUTE_UNAVAILABLE, whose card tells the asker the agent is unavailable
// and who to ask. Every other cause (authority, thread, documents, budget) keeps
// CONTEXT_UNAVAILABLE.
func personaRunModelWorkFailureCode(err error) string {
	if errors.Is(err, errPersonaModelRouteUnavailable) {
		return "MODEL_ROUTE_UNAVAILABLE"
	}
	return "CONTEXT_UNAVAILABLE"
}
