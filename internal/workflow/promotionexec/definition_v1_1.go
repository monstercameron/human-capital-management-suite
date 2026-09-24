package promotionexec

import "github.com/monstercameron/human-capital-management-suite/internal/workflow"

// Version 1.1.0 is frozen at definition version 2 and IR schema v1. Its
// digest is persisted by already-published workflow-version records.
const (
	VersionV1_1         = 2
	SemanticVersionV1_1 = "1.1.0"
)

// DefinitionV1_1 returns the immutable graph published as 1.1.0.
func DefinitionV1_1() workflow.Definition {
	return promotionDefinition(VersionV1_1, true)
}

// CompileV1_1 reproduces the exact schema-v1 plan pinned by 1.1.0 records.
func CompileV1_1(definitions ...workflow.Definition) (*workflow.CompiledWorkflow, error) {
	def := DefinitionV1_1()
	if len(definitions) == 1 {
		def = definitions[0]
	}
	return workflow.Compile(def, workflow.Options{Phase: workflow.PhaseP1B, Capabilities: frozenV1CapabilitySnapshot{}, IRSchemaVersion: 1})
}

// CompileSimulationV1_1 returns the frozen zero-effect SIMULATE projection.
func CompileSimulationV1_1() (*workflow.CompiledWorkflow, error) {
	return workflow.Compile(DefinitionV1_1(), workflow.Options{Phase: workflow.PhaseP1B, Capabilities: frozenV1CapabilitySnapshot{}, SimulateProjection: true, IRSchemaVersion: 1})
}

// NodeOrderV1_1 is the compiler reachability order of the frozen 1.1.0 plan.
func NodeOrderV1_1() []string {
	return NodeOrder()
}
