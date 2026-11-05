package agentcost

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
)

// Agentcost001LinesFromRunRecords rebuilds per-call ledger lines from sealed
// run records: every line each run sealed, oldest call first within a run,
// runs in the order given. A line that no longer validates, or one sealed
// under the wrong tenant, agent or run, fails the whole rebuild: the spend
// report never silently drops spend.
func Agentcost001LinesFromRunRecords(runs []runstate.Run) ([]agentmodel.CallLine, error) {
	var lines []agentmodel.CallLine
	for _, run := range runs {
		for _, line := range run.Calls {
			if err := line.Validate(); err != nil {
				return nil, fmt.Errorf("agentcost: run %q seals an invalid line: %v", run.ID, err)
			}
			if line.TenantID != run.TenantID || line.AgentID != run.AgentID || line.RunID != run.ID {
				return nil, fmt.Errorf("agentcost: run %q seals a line for %q/%q/%q", run.ID, line.TenantID, line.AgentID, line.RunID)
			}
			lines = append(lines, line)
		}
	}
	return lines, nil
}
