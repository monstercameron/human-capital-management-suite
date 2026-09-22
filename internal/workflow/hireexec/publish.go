package hireexec

import "github.com/monstercameron/human-capital-management-suite/internal/workflow"

// SemanticVersion is the version New employee hire is published under.
const SemanticVersion = "1.0.0"

// CompileOptions are the options Compile uses. Publication recompiles the
// definition to prove the plan it is handed, so a publisher needs the same
// phase and capability records rather than a copy of them.
func CompileOptions() workflow.Options {
	return workflow.Options{Phase: workflow.PhaseP1B, Capabilities: capabilities()}
}
