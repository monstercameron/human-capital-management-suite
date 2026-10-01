package releasefixture

import (
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/leavereturn"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/recruit"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/transfer"
)

// ReferenceConformance is a read-only catalog entry for a reference workflow
// whose semantics are proven before promotion. It deliberately exposes the
// typed definition only: this catalog grants no execution authority and does
// not turn a SIMULATE fixture into an ATS, payroll or leave integration.
type ReferenceConformance struct {
	ID         string
	WorkflowID string
	Version    uint32
	Definition func() workflow.Definition
}

// ReferenceConformanceCatalog names the conformance references carried by the
// served workflow fixture package. Keeping the catalog here makes the
// references part of the shipped command's dependency closure while leaving
// release approval and activation to the existing governed registry path.
func ReferenceConformanceCatalog() []ReferenceConformance {
	return []ReferenceConformance{
		{ID: "CONF-002", WorkflowID: recruit.WorkflowID, Version: recruit.Version, Definition: recruit.ReferenceDefinition},
		{ID: "CONF-003", WorkflowID: transfer.WorkflowID, Version: transfer.Version, Definition: transfer.ReferenceDefinition},
		{ID: "CONF-004", WorkflowID: leavereturn.WorkflowID, Version: leavereturn.Version, Definition: leavereturn.ReferenceDefinition},
	}
}
