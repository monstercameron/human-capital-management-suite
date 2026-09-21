// Package cicd contains the repository-independent contract for the
// authoritative clean-checkout Go verification pipeline.
package cicd

import (
	"fmt"
	"sort"
	"strings"
)

// RequiredCommands is the complete composition-root matrix for CICD-001.
// Keeping this list in one place prevents a pipeline from silently dropping a
// binary when a new verification job is added.
var RequiredCommands = []string{
	"cmd/hcmnext",
	"cmd/worker",
	"cmd/projector",
	"cmd/migrate",
	"cmd/hcmctl",
	"cmd/scheduler",
	"cmd/frontenddev",
}

// Pipeline describes the observable inputs of the clean-checkout gate.
// Commands are package paths built by the pipeline; DeferredPackages are
// packages discovered outside the Phase 1 graph; LegacyNodeAuthoritative is
// true when Node/npm is a required delivery or verification runtime.
type Pipeline struct {
	Commands                []string
	DeferredPackages        []string
	LegacyNodeAuthoritative bool
	GenerationDrift         bool
	ArchitectureDrift       bool
	FormattingDrift         bool
	VetFailed               bool
	StaticcheckFailed       bool
	UnitFailed              bool
	IntegrationFailed       bool
	RaceFailed              bool
	FuzzFailed              bool
}

// Finding is a stable, machine-readable gate diagnostic.
type Finding struct {
	Code   string
	Detail string
}

func (f Finding) Error() string { return fmt.Sprintf("CICD-001 %s: %s", f.Code, f.Detail) }

// Check applies the CICD-001 contract. Findings are sorted by code so CI
// annotations and golden reports remain deterministic.
func Check(p Pipeline) []Finding {
	want := make(map[string]bool, len(RequiredCommands))
	for _, command := range RequiredCommands {
		want[command] = true
	}
	seen := make(map[string]bool, len(p.Commands))
	var out []Finding
	for _, command := range p.Commands {
		if !want[command] {
			out = append(out, Finding{"unknown-command", command})
			continue
		}
		if seen[command] {
			out = append(out, Finding{"duplicate-command", command})
		}
		seen[command] = true
	}
	for _, command := range RequiredCommands {
		if !seen[command] {
			out = append(out, Finding{"missing-command", command})
		}
	}
	if p.LegacyNodeAuthoritative {
		out = append(out, Finding{"legacy-node-authoritative", "Node/npm is required by the authoritative pipeline"})
	}
	if p.GenerationDrift {
		out = append(out, Finding{"generation-drift", "generated output differs from pinned generation"})
	}
	if p.ArchitectureDrift {
		out = append(out, Finding{"architecture-drift", "import-boundary or Phase 1 architecture check failed"})
	}
	if len(p.DeferredPackages) > 0 {
		packages := append([]string(nil), p.DeferredPackages...)
		sort.Strings(packages)
		out = append(out, Finding{"deferred-package", strings.Join(packages, ", ")})
	}
	for code, failed := range map[string]bool{
		"format-failed": p.FormattingDrift, "vet-failed": p.VetFailed,
		"staticcheck-failed": p.StaticcheckFailed, "unit-failed": p.UnitFailed,
		"integration-failed": p.IntegrationFailed, "race-failed": p.RaceFailed,
		"fuzz-failed": p.FuzzFailed,
	} {
		if failed {
			out = append(out, Finding{code, "required Go verification class failed"})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// CleanPipeline returns the canonical passing fixture used by callers that
// need to assemble a pipeline incrementally.
func CleanPipeline() Pipeline { return Pipeline{Commands: append([]string(nil), RequiredCommands...)} }
