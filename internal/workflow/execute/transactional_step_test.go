package execute

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// txStepRunner claims one node for in-transaction execution and records how
// the driver invoked it.
type txStepRunner struct {
	claim      string
	fail       error
	wrongNode  bool
	runCalls   int
	txCalls    int
	sawEx      runtime.Executor
	advancesAt int
	recordedAt time.Time
	scn        *obsScenario
}

func (r *txStepRunner) Run(_ context.Context, req StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	r.runCalls++
	return frontier.NodeOutcome{NodeID: req.Node.ID, Outcome: workflow.OutcomeSucceeded, OutputDigest: "sha256:end-output"}, runtime.GovernanceRefs{}, nil
}

func (r *txStepRunner) RunsInTransaction(node workflow.CompiledNode) bool { return node.ID == r.claim }

func (r *txStepRunner) RunInTx(_ context.Context, ex runtime.Executor, req StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	r.txCalls++
	r.sawEx = ex
	r.advancesAt = r.scn.advanceCalls
	r.recordedAt = req.RecordedAt
	if r.fail != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, r.fail
	}
	id := req.Node.ID
	if r.wrongNode {
		id = "other"
	}
	return frontier.NodeOutcome{NodeID: id, Outcome: workflow.OutcomeSucceeded, OutputDigest: "sha256:end-output"}, runtime.GovernanceRefs{}, nil
}

// TestTransactionalStepRunsInsideTheAdvanceTransaction proves a node a
// TransactionalStepRunner claims runs inside the advance transaction that
// records it -- after the previous advancement, on the same executor, never
// through Run -- and that a failure or mismatched outcome aborts the
// advancement so no write survives.
func TestTransactionalStepRunsInsideTheAdvanceTransaction(t *testing.T) {
	ctx := context.Background()

	scn := newOBSScenario(t, "")
	runner := &txStepRunner{claim: "end", scn: scn}
	d := scn.driver(t)
	d.opts.Steps = runner
	result, err := d.Resume(ctx, scn.req)
	if err != nil || result.Status != StatusComplete {
		t.Fatalf("Resume = %+v, %v", result, err)
	}
	tx := d.opts.DB.(oneBeginner).tx
	if runner.runCalls != 0 || runner.txCalls != 1 || runner.sawEx != runtime.Executor(tx) || runner.advancesAt != 1 {
		t.Fatalf("runner = run %d, tx %d, same executor %v, advances before step %d; want 0, 1, true, 1",
			runner.runCalls, runner.txCalls, runner.sawEx == runtime.Executor(tx), runner.advancesAt)
	}
	if spans := scn.instrumentation.spansNamed("node"); len(spans) != 1 || spans[0].attrs.NodeID != "end" || spans[0].outcome != OutcomeSuccess {
		t.Fatalf("node spans = %+v", spans)
	}

	for name, tc := range map[string]struct {
		runner *txStepRunner
		want   string
	}{
		"step failure":      {&txStepRunner{claim: "end", fail: errors.New("domain write refused")}, "in transaction"},
		"mismatched output": {&txStepRunner{claim: "end", wrongNode: true}, "while running end"},
	} {
		scn := newOBSScenario(t, "")
		tc.runner.scn = scn
		d := scn.driver(t)
		d.opts.Steps = tc.runner
		if _, err := d.Resume(ctx, scn.req); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: Resume error = %v, want %q", name, err, tc.want)
		}
		if scn.advanceCalls != 1 {
			t.Fatalf("%s: advance calls = %d, want only the approve advancement", name, scn.advanceCalls)
		}
		if spans := scn.instrumentation.spansNamed("node"); len(spans) != 1 || spans[0].outcome != OutcomeFailure {
			t.Fatalf("%s: node spans = %+v, want one failed span", name, spans)
		}
	}

	// A runner that does not claim the node keeps the pre-transaction path.
	scn = newOBSScenario(t, "")
	unclaimed := &txStepRunner{claim: "approve", scn: scn}
	d = scn.driver(t)
	d.opts.Steps = unclaimed
	if _, err := d.Resume(ctx, scn.req); err != nil || unclaimed.runCalls != 1 || unclaimed.txCalls != 0 {
		t.Fatalf("unclaimed node: err %v, run %d, tx %d", err, unclaimed.runCalls, unclaimed.txCalls)
	}
}

// TestTransactionalDecisionReceivesDriverClock proves a raise_threshold-like
// transactional decision receives the driver's recorded instant unchanged.
// That instant is used by the domain adapter to bound delegated-principal
// validity, so a zero value must not be manufactured at this boundary.
func TestTransactionalDecisionReceivesDriverClock(t *testing.T) {
	scn := newOBSScenario(t, "")
	// The second node stands in for promotion's transactional raise_threshold
	// decision: it is dispatched through RunInTx rather than StepRunner.Run.
	scn.req.Start.Resolver.(staticResolver).selection.Plan.Nodes[1].Type = workflow.StepDecision
	// The resolver's selection and the test scenario share this compiled plan.
	at := time.Date(2026, 9, 23, 16, 45, 12, 345000000, time.UTC)
	runner := &txStepRunner{claim: "end", scn: scn}
	d := scn.driver(t)
	d.opts.Steps = runner
	d.opts.Clock = func() time.Time { return at }
	if _, err := d.Resume(context.Background(), scn.req); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if runner.txCalls != 1 || !runner.recordedAt.Equal(at) {
		t.Fatalf("transactional decision calls=%d RecordedAt=%s; want one call at %s", runner.txCalls, runner.recordedAt.Format(time.RFC3339Nano), at.Format(time.RFC3339Nano))
	}
}
