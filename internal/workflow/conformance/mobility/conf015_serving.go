package mobility

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// ValidateServingContract checks that the simulation workflow can be composed
// by the serving cell. The check is intentionally read-only: CONF-015 remains
// exploratory and never grants production mobility authority.
func ValidateServingContract() error {
	setup, err := NewSetup(GoldenEnvironment())
	if err != nil {
		return fmt.Errorf("mobility: serving contract: %w", err)
	}
	if setup == nil || setup.Plan == nil || setup.Plan.WorkflowID != WorkflowID {
		return fmt.Errorf("mobility: serving contract compiled the wrong workflow")
	}
	if setup.Plan.TerminalProfile != workflow.TerminalProfileSimulateOnly {
		return fmt.Errorf("mobility: serving contract is not simulation-only")
	}
	if setup.Options.Capabilities == nil {
		return fmt.Errorf("mobility: serving contract has no capability registry")
	}
	return nil
}
