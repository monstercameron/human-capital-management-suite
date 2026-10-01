package application

import (
	"fmt"

	connectivityedge "github.com/monstercameron/human-capital-management-suite/internal/connectivity/edge"
)

// qualifyServedEdge is the application-side adapter for EDGE-010. The edge
// package owns the concrete implementation record and validation; the serve
// composition owns the decision to refuse publication when qualification
// fails. No endpoint authorization semantics are defined here.
func qualifyServedEdge() (connectivityedge.Evidence, error) {
	evidence, err := connectivityedge.QualifyProduction()
	if err != nil {
		return connectivityedge.Evidence{}, fmt.Errorf("edge qualification failed: %w", err)
	}
	if !evidence.Ready || evidence.Status != "READY" {
		return connectivityedge.Evidence{}, fmt.Errorf("edge qualification is not ready: status=%s blockers=%v human_inputs=%v", evidence.Status, evidence.Blockers, evidence.HumanInputs)
	}
	return evidence, nil
}
