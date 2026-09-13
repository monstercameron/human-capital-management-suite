package convergence

import (
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/closurewitness"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/designclosure"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/gateevidence/selectionbind"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/intentcoverage"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowmaturity"
)

// Adapters reduce each existing compiler's own report to observations. They
// map finding codes onto the contract vocabulary and owners onto the exact
// definition or todo the compiler already names; they never parse wording.
// A code with no mapping yields an empty contract, which Converge surfaces
// as UNMAPPED_FINDING_CODE instead of dropping.

var closureClassContract = map[closurewitness.EdgeClass]Contract{
	closurewitness.ClassSource:     ContractSource,
	closurewitness.ClassPhaseGate:  ContractPhaseGate,
	closurewitness.ClassSlice:      ContractSlice,
	closurewitness.ClassModel:      ContractModel,
	closurewitness.ClassEngine:     ContractEngine,
	closurewitness.ClassCapability: ContractCapability,
	closurewitness.ClassHandler:    ContractHandler,
	closurewitness.ClassEndpoint:   ContractEndpoint,
	closurewitness.ClassScenario:   ContractScenario,
	closurewitness.ClassTest:       ContractTest,
	closurewitness.ClassTodo:       ContractTodo,
	closurewitness.ClassEvidence:   ContractEvidence,
}

// FromClosureWitness maps SLICE-016 witness defects onto their definition
// and orphan edges onto ownerless gaps keyed by the edge identity.
func FromClosureWitness(report closurewitness.Report) []Observation {
	var out []Observation
	for _, w := range report.Witnesses {
		for _, d := range w.Defects {
			out = append(out, Observation{Compiler: CompilerClosureWitness, Code: d.Code, Owner: w.Definition, Contract: closureClassContract[d.Class], Detail: d.Detail})
		}
	}
	for _, d := range report.Orphans {
		out = append(out, Observation{Compiler: CompilerClosureWitness, Code: d.Code, Contract: closureClassContract[d.Class], Subject: d.Identity, Detail: d.Detail})
	}
	return out
}

// ClosureWitnessUnknowns names every edge class the witness loader could
// not source: its absence is an unknown, never a clean class.
func ClosureWitnessUnknowns(snap closurewitness.Snapshot) []Unknown {
	var out []Unknown
	for _, class := range snap.Unsourced {
		out = append(out, Unknown{Code: UnknownInputUnsourced, Ref: CompilerClosureWitness + ":" + string(class), Detail: "the registry behind this closure edge class is missing, so its gaps cannot be enumerated"})
	}
	return out
}

var designFindingContract = map[string]Contract{
	designclosure.MissingSource:       ContractSource,
	designclosure.MissingOwner:        ContractOwner,
	designclosure.MissingPhase:        ContractPhaseGate,
	designclosure.MissingArtifact:     ContractWorkflowDesign,
	designclosure.MissingTest:         ContractTest,
	designclosure.MissingEvidence:     ContractEvidence,
	designclosure.MissingExpiry:       ContractDecision,
	designclosure.TickWithoutEvidence: ContractEvidence,
	designclosure.IncompleteCoverage:  ContractSource,
	designclosure.UnknownItem:         ContractSource,
	designclosure.DuplicateItem:       ContractSource,
}

// designBlockerContract maps CLOSE-001 row blockers that name missing work.
// Open-todo, deferral and rejection blockers already have an owner or a
// decision and are not gaps.
var designBlockerContract = map[string]Contract{
	"delivery":  ContractTodo,
	"selection": ContractTodo,
	"gap":       ContractCapability,
	"test":      ContractTest,
}

// FromDesignClosure maps CLOSE-001 findings and missing-work blockers onto
// their scope item.
func FromDesignClosure(register designclosure.Register, findings []designclosure.Finding) []Observation {
	var out []Observation
	for _, f := range findings {
		out = append(out, Observation{Compiler: CompilerDesignClosure, Code: f.Code, Owner: f.Item, Contract: designFindingContract[f.Code], Detail: f.Detail})
	}
	for _, row := range register.Rows {
		for _, b := range row.Blockers {
			contract, ok := designBlockerContract[b.Kind]
			if !ok {
				continue
			}
			out = append(out, Observation{Compiler: CompilerDesignClosure, Code: "BLOCKER_" + strings.ToUpper(b.Kind), Owner: row.Item, Contract: contract, Detail: b.Detail + ": " + b.Ref})
		}
	}
	return out
}

var intentOrphanContract = map[string]Contract{
	intentcoverage.KindIntentContract:         ContractCapability,
	intentcoverage.KindIntentModel:            ContractModel,
	intentcoverage.KindIntentProperty:         ContractProperty,
	intentcoverage.KindIntentGovernance:       ContractGovernance,
	intentcoverage.KindIntentCapability:       ContractCapability,
	intentcoverage.KindIntentWorkflowOrDirect: ContractTodo,
	intentcoverage.KindIntentTest:             ContractTest,
	intentcoverage.KindIntentEvidence:         ContractEvidence,
	intentcoverage.KindGapDeferment:           ContractCapability,
	intentcoverage.KindTodoDirectDangling:     ContractTodo,
	intentcoverage.KindFalseMaturityClaim:     ContractPhaseGate,
}

// FromIntentCoverage maps GOV-026 orphans (allowlisted or not: an allowlist
// names an accountable team, not a closing todo) onto their accepted intent.
// Feature-keyed orphans resolve to the feature's bound intent; an orphan
// that names no accepted intent is ownerless and keyed by its kind and id.
func FromIntentCoverage(report intentcoverage.Report, snap intentcoverage.Snapshot) []Observation {
	accepted := map[string]bool{}
	for _, in := range snap.Intents {
		accepted[in.ID] = true
	}
	featureIntent := map[string]string{}
	for _, g := range snap.Gaps {
		featureIntent[g.FeatureID] = g.IntentID
	}
	var out []Observation
	for _, o := range report.Orphans {
		obs := Observation{Compiler: CompilerIntentCoverage, Code: o.Kind, Contract: intentOrphanContract[o.Kind], Detail: o.Detail}
		switch {
		case accepted[o.ID]:
			obs.Owner = o.ID
		case accepted[featureIntent[o.ID]]:
			obs.Owner = featureIntent[o.ID]
		default:
			obs.Subject = o.Kind + ":" + o.ID
		}
		out = append(out, obs)
	}
	return out
}

var maturityBlockerContract = map[string]Contract{
	workflowmaturity.UnboundDesign:              ContractWorkflowDesign,
	workflowmaturity.UnresolvedReference:        ContractWorkflowDesign,
	workflowmaturity.UnjustifiedOmission:        ContractWorkflowDesign,
	workflowmaturity.CatalogNameOnly:            ContractWorkflowDesign,
	workflowmaturity.MissingAdversarialScenario: ContractScenario,
	workflowmaturity.DanglingTodoEdge:           ContractTodo,
	workflowmaturity.DanglingTestEdge:           ContractTest,
	workflowmaturity.DanglingEvidenceEdge:       ContractEvidence,
	workflowmaturity.UnresolvedDecision:         ContractDecision,
}

// FromWorkflowMaturity maps WF-DISC-012 blockers onto their definition.
func FromWorkflowMaturity(report workflowmaturity.Report) []Observation {
	var out []Observation
	for _, r := range report.Results {
		for _, b := range r.Blockers {
			out = append(out, Observation{Compiler: CompilerWorkflowMaturity, Code: b.Code, Owner: r.Definition, Contract: maturityBlockerContract[b.Code], Detail: b.Detail})
		}
	}
	return out
}

// SelectionManifestOwner owns reasons against the P1A manifest itself.
const SelectionManifestOwner = "NEXT-002"

// FromSelectionBind maps NEXT-002's selection-completeness report onto the
// selecting todos. Every reason a binding is not ready is one observation of
// that todo's SELECTION_GATE contract; an unfilled slot is one observation
// for each filler the ceiling names (so it deduplicates with the binding's
// own reasons) plus one SELECTION_SLOT_UNFILLED unknown.
func FromSelectionBind(report selectionbind.Report) ([]Observation, []Unknown) {
	var out []Observation
	var unknowns []Unknown
	for _, reason := range report.ManifestReasons {
		out = append(out, Observation{Compiler: CompilerSelectionBind, Code: "MANIFEST_REASON", Owner: SelectionManifestOwner, Contract: ContractSelectionGate, Detail: reason})
	}
	for _, b := range report.Bindings {
		for _, reason := range b.Reasons {
			out = append(out, Observation{Compiler: CompilerSelectionBind, Code: "BINDING_NOT_READY", Owner: b.TodoID, Contract: ContractSelectionGate, Detail: reason})
		}
	}
	for _, s := range report.Slots {
		if s.Filled {
			continue
		}
		unknowns = append(unknowns, Unknown{Code: UnknownSlotUnfilled, Ref: "slot:" + s.Slot, Detail: fmt.Sprintf("PHASE-001 selection slot %s is unfilled: %s", s.Slot, strings.Join(s.Reasons, "; "))})
		owners := s.FillingTodoIDs
		if len(owners) == 0 {
			out = append(out, Observation{Compiler: CompilerSelectionBind, Code: "SLOT_UNFILLED", Contract: ContractSelectionGate, Subject: "slot:" + s.Slot, Detail: strings.Join(s.Reasons, "; ")})
		}
		for _, owner := range owners {
			out = append(out, Observation{Compiler: CompilerSelectionBind, Code: "SLOT_UNFILLED", Owner: owner, Contract: ContractSelectionGate, Detail: fmt.Sprintf("slot %s: %s", s.Slot, strings.Join(s.Reasons, "; "))})
		}
	}
	return out, unknowns
}
