package promotionexec

import (
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// Capability identities the executable graph invokes, exported for the
// composition that binds a governed invocation to each node (WF-RUN-034).
const (
	CapabilitySnapshotWorker        = capSnapshotWorker
	CapabilitySimulateCompensation  = capSimulate
	CapabilityEvaluateBand          = capEvaluateBand
	CapabilityRevalidate            = capRevalidate
	CapabilityExecutePromotion      = capExecute
	CapabilityObservePayroll        = capObservePayroll
	CapabilityObserveAccess         = capObserveAccess
	CapabilityObserveReconciliation = capObserveRecon
)

// GovernedReadDefinitions returns the EXECUTE-mode definitions of the four
// read-only capabilities the graph invokes over local stores and that the
// P1A bootstrap table does not publish: revalidation and the three
// observations. They are the same records [Compile] resolves, completed with
// the plan's risk class so a capability registry accepts them.
func GovernedReadDefinitions() []capability.Definition {
	resolver := capabilities(workflow.ModeExecute)
	out := make([]capability.Definition, 0, 4)
	for _, id := range []string{capRevalidate, capObservePayroll, capObserveAccess, capObserveRecon} {
		record, _ := resolver.Lookup(capability.Key{ID: id, Version: 1})
		def := record.Definition
		def.RiskClass = Definition().RiskClass
		out = append(out, def)
	}
	return out
}
