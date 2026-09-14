package effects

import (
	"context"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// PolicyEntry is one row of a [PolicyResolver]'s table: the exact workflow,
// pin and compiled plan a start request resolves to when Match accepts it.
//
// Match is the "intent type" key expressed as a predicate rather than a
// single string field: [runtime.StartRequest] itself carries no intent-type
// column (only the exact bound [runtime.ProposalBinding] and the caller's
// own declared context), so the composition root supplies, as plain data,
// however it tells one intent type apart from another. Nothing here embeds
// a graph: every entry only names which already-published
// workflow/pin/plan a matching request binds.
type PolicyEntry struct {
	WorkflowID string
	Pin        version.Pin
	Plan       *workflow.CompiledWorkflow
	// Match reports whether this entry answers req. A nil Match always
	// matches, which is correct for a composition with exactly one entry.
	Match func(req runtime.StartRequest) bool
}

// PolicyResolver is a [runtime.WorkflowResolver] backed by a small ordered
// table of [PolicyEntry] values, supplied as data by the composition root
// -- never a workflow graph a business service embeds. The first entry
// whose Match accepts the request wins; a request no entry accepts is
// refused rather than defaulted to whichever entry happens to be first.
type PolicyResolver struct {
	Entries []PolicyEntry
}

var _ runtime.WorkflowResolver = PolicyResolver{}

// ResolveWorkflow implements [runtime.WorkflowResolver].
func (r PolicyResolver) ResolveWorkflow(_ context.Context, req runtime.StartRequest) (runtime.WorkflowSelection, error) {
	for _, e := range r.Entries {
		if e.Match != nil && !e.Match(req) {
			continue
		}
		if e.WorkflowID == "" || e.Plan == nil {
			return runtime.WorkflowSelection{}, fmt.Errorf(
				"effects: policy entry for %q names no workflow id or compiled plan", e.WorkflowID)
		}
		return runtime.WorkflowSelection{WorkflowID: e.WorkflowID, Pin: e.Pin, Plan: e.Plan}, nil
	}
	return runtime.WorkflowSelection{}, fmt.Errorf(
		"effects: no policy entry matches this start request (correlation %q)", req.CorrelationID)
}

// ResolveWorkflowInTx adapts the immutable, exact-pin policy table to the
// transaction-bound resolver contract. PolicyResolver has no database-backed
// facts to read and therefore makes no database-freshness claim; the non-nil
// transaction only proves the caller invoked selection inside the START
// boundary. Approval, supersession and other current facts remain separate
// transaction-bound ports owned by runtime.Start.
func (r PolicyResolver) ResolveWorkflowInTx(ctx context.Context, tx dbport.Tx, req runtime.StartRequest) (ret0 runtime.WorkflowSelection, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.effects.resolve_workflow")
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if tx == nil {
		return runtime.WorkflowSelection{}, fmt.Errorf("effects: static policy resolution requires an active transaction boundary")
	}
	return r.ResolveWorkflow(ctx, req)
}
