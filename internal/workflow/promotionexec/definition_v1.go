package promotionexec

import "github.com/monstercameron/human-capital-management-suite/internal/workflow"

// Version 1.0.0 of the promotion graph, frozen.
//
// 1.1.0 ([Definition]) inserts the two provider-confirmation waits. Instances
// started on 1.0.0 stay pinned to its compiled-plan digest for their whole
// life, so the composition keeps serving this exact plan beside 1.1.0: it must
// keep compiling to the digest those instances pinned
// (655535f1e484991a79562a292eb21374381b1e757e115a3ec53eb25ce61679d7), and the
// package golden test holds it there. Never edit the 1.0.0 shape; publish a
// new version instead.
const (
	// VersionV1_0 is the frozen definition version of 1.0.0.
	VersionV1_0 = 1
	// SemanticVersionV1_0 is the frozen graph's published identity.
	SemanticVersionV1_0 = "1.0.0"
)

// DefinitionV1_0 returns the frozen 1.0.0 promotion graph: execute_promotion
// routes straight to observe_payroll and nothing waits on a provider.
func DefinitionV1_0() workflow.Definition {
	return promotionDefinition(VersionV1_0, false)
}

// CompileV1_0 compiles the EXECUTE projection of the frozen 1.0.0 graph. An
// optional definition is accepted for mutation tests, as [Compile] does.
func CompileV1_0(definitions ...workflow.Definition) (*workflow.CompiledWorkflow, error) {
	def := DefinitionV1_0()
	if len(definitions) == 1 {
		def = definitions[0]
	}
	return Compile(def)
}

// CompileSimulationV1_0 compiles the zero-effect SIMULATE projection of the
// frozen 1.0.0 graph.
func CompileSimulationV1_0() (*workflow.CompiledWorkflow, error) {
	return CompileSimulation(DefinitionV1_0())
}

// NodeOrderV1_0 is the compiler reachability order of the frozen 1.0.0 plan.
func NodeOrderV1_0() []string {
	ids := []string{NodeSnapshotWorker, NodeSimulateCompensation, NodeEvaluateBand, NodeRaiseThreshold, NodeApproveFinance, NodeApproveManager, NodeWaitEffectiveDate, NodeRevalidate, NodeStillValid, NodeExecutePromotion, NodeReapproval, NodeEndBlocked, NodeObservePayroll, NodeEndInvalidated, NodeEndRejected, NodeEndExpired, NodeObserveAccess, NodeObserveReconciliation, NodeCompensateHold, NodeAcknowledgeRelease, NodeEndComplete, NodeEndRepairPlan, NodeEndCancelled}
	return append([]string(nil), ids...)
}
