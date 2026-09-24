package hireexec

import "github.com/monstercameron/human-capital-management-suite/internal/workflow"

// SemanticVersionV1_0 is the semantic identity of the frozen published plan.
const SemanticVersionV1_0 = "1.0.0"

// SemanticVersion is the current New employee hire semantic version.
const SemanticVersion = "1.1.0"

// DefinitionV1_0 returns the exact definition version used by the published
// 1.0.0 plan. Its bytes and compiled digest are kept available for pinned
// records and in-flight runs.
func DefinitionV1_0() workflow.Definition {
	return definition(VersionV1_0)
}

// CompileV1_0 reproduces the frozen 1.0.0 plan using its original IR schema.
func CompileV1_0(definitions ...workflow.Definition) (*workflow.CompiledWorkflow, error) {
	def := DefinitionV1_0()
	if len(definitions) == 1 {
		def = definitions[0]
	}
	return workflow.Compile(def, CompileOptionsV1_0())
}

// CompileOptions are the options Compile uses. Publication recompiles the
// definition to prove the plan it is handed, so a publisher needs the same
// phase and capability records rather than a copy of them.
func CompileOptions() workflow.Options {
	return workflow.Options{Phase: workflow.PhaseP1B, Capabilities: capabilities(), IRSchemaVersion: workflow.CurrentIRSchemaVersion}
}

// CompileOptionsV1_0 preserves the exact schema used by published 1.0.0.
func CompileOptionsV1_0() workflow.Options {
	return workflow.Options{Phase: workflow.PhaseP1B, Capabilities: capabilities(), IRSchemaVersion: 1}
}
