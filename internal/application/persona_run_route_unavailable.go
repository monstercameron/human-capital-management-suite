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

// errPersonaModelRouteExpired marks the lapsed case of the above: a deployment
// was published for the exact manifest but its window passed. The asker is
// told the setup expired and who can renew it (AGENTUX-082).
var errPersonaModelRouteExpired = errors.New("application: current tenant model route expired")

// personaRunModelWorkFailureCode is the terminal code of a run whose model work
// could not be built. A lapsed route ends the run as MODEL_ROUTE_EXPIRED,
// whose card tells the asker the agent's setup expired and who can renew it; a
// route that is missing or invalid ends it as MODEL_ROUTE_UNAVAILABLE, whose
// card tells the asker the agent is unavailable and who to ask. Every other
// cause (authority, thread, documents, budget) keeps CONTEXT_UNAVAILABLE.
func personaRunModelWorkFailureCode(err error) string {
	if errors.Is(err, errPersonaModelRouteExpired) {
		return "MODEL_ROUTE_EXPIRED"
	}
	if errors.Is(err, errPersonaModelRouteUnavailable) {
		return "MODEL_ROUTE_UNAVAILABLE"
	}
	return "CONTEXT_UNAVAILABLE"
}
