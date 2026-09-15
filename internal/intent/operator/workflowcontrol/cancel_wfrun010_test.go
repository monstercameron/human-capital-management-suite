package workflowcontrol

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/cancellation"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func (f *fixture) cancellationDecisions(id uuid.UUID) []cancellation.Record {
	f.t.Helper()
	var recs []cancellation.Record
	f.tx(func(tx dbport.Tx) error {
		var err error
		recs, err = cancellation.Decisions(context.Background(), tx, f.tenant, id)
		return err
	})
	return recs
}

// TestTodo_WF_RUN_010_OperatorCancel proves the operator cancel control runs
// the governed decision over durable state: a compensable produced effect is
// APPLIED as COMPENSATION_REQUIRED with the instance CANCELLING (never
// CANCELLED) and the obligation recorded; a child that cannot stop refuses
// the parent as TOO_LATE with the refusal committed and both instances
// untouched.
func TestTodo_WF_RUN_010_OperatorCancel(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	compensable, err := promotionexec.Compile()
	if err != nil {
		t.Fatal(err)
	}
	for i := range compensable.Nodes {
		if compensable.Nodes[i].ID == promotionexec.NodeExecutePromotion {
			compensable.Nodes[i].CompensationRef = &workflow.ResolvedReference{Kind: workflow.RefCompensation, ID: "compensation.promotion.reverse", Version: "1"}
		}
	}
	c := f.controllerWith(operator.NewMemoryJournal(), plans{compensable}, f.conn)
	id, v := f.instance([]string{"observe_payroll"}, node{"execute_promotion", []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeSucceeded}})
	res, err := c.Cancel(ctx, f.cmd(id, v, "cancel-compensate"))
	expect(t, "cancel over a compensable effect", res, err, OutcomeApplied, CodeCompensationRequired)
	if res.InstanceStatus != runtime.InstanceCancelling || res.NodeID != "execute_promotion" {
		t.Fatalf("compensation result = %+v", res)
	}
	if inst, _ := f.load(id); inst.RuntimeStatus != runtime.InstanceCancelling || inst.CompletedAt != nil {
		t.Fatalf("instance = %+v", inst)
	}
	if recs := f.cancellationDecisions(id); len(recs) != 1 || recs[0].Decision != workflow.CompensationRequired ||
		len(recs[0].CompensationRefs) != 1 || !strings.HasSuffix(recs[0].CompensationRefs[0], "compensation.promotion.reverse@1") {
		t.Fatalf("obligation = %+v", recs)
	}

	// A parent whose child already committed an irreversible effect.
	plain := f.controller(operator.NewMemoryJournal())
	parent, pv := f.instance([]string{"approve_manager"})
	child, cv := f.instance([]string{"observe_payroll"}, node{"execute_promotion", []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeSucceeded}})
	f.tx(func(tx dbport.Tx) error {
		return (runtimestate.ChildLinkStore{}).Link(ctx, tx, runtimestate.ChildLink{TenantID: f.tenant, Parent: parent, Child: child,
			ParentNodeID: "approve_manager", Ordinal: 1, Mode: runtimestate.ChildAwait, InputDigest: strings.Repeat("d", 64), CreatedAt: fixedNow})
	})
	refused, err := plain.Cancel(ctx, f.cmd(parent, pv, "cancel-parent"))
	expect(t, "cancel over an uncancellable child", refused, err, OutcomeTooLate, CodeChildNotCancellable)
	if pAfter, _ := f.load(parent); pAfter.InstanceVersion != pv || pAfter.RuntimeStatus != runtime.InstanceRunning {
		t.Fatalf("refused parent = %+v", pAfter)
	}
	if cAfter, _ := f.load(child); cAfter.InstanceVersion != cv {
		t.Fatalf("refused child moved %d -> %d", cv, cAfter.InstanceVersion)
	}
	if recs := f.cancellationDecisions(parent); len(recs) != 1 || recs[0].Decision != workflow.CannotCancel {
		t.Fatalf("a TOO_LATE refusal must be committed: %+v", recs)
	}
}

// TestTodo_WF_RUN_010_OperatorCancelRace proves concurrent operator cancels
// under different idempotency keys, each on its own connection, converge on
// one governed decision and one transition.
func TestTodo_WF_RUN_010_OperatorCancelRace(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	id, v := f.instance([]string{"approve_manager"})
	const workers = 5
	results := make([]Result, workers)
	errs := make([]error, workers)
	controllers := make([]*Controller, workers)
	for i := range controllers {
		controllers[i] = f.controllerWith(operator.NewMemoryJournal(), plans{f.plan}, f0conn(t, f.db))
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = controllers[i].Cancel(ctx, f.cmd(id, v, "race-"+uuid.NewString()))
		}(i)
	}
	close(start)
	wg.Wait()
	applied := 0
	for i, r := range results {
		if errs[i] != nil {
			t.Fatalf("worker %d: %v", i, errs[i])
		}
		switch {
		case r.Outcome == OutcomeApplied && r.InstanceStatus == runtime.InstanceCancelled:
			applied++
		case r.Outcome == OutcomeTooLate && r.Code == CodeInstanceTerminal:
		default:
			t.Fatalf("worker %d = %+v", i, r)
		}
	}
	if applied == 0 {
		t.Fatal("no worker reported the cancellation")
	}
	if recs := f.cancellationDecisions(id); len(recs) != 1 {
		t.Fatalf("decision rows = %d, want 1", len(recs))
	}
	if inst, _ := f.load(id); inst.InstanceVersion != v+2 || inst.RuntimeStatus != runtime.InstanceCancelled {
		t.Fatalf("instance = %s v%d, want CANCELLED v%d", inst.RuntimeStatus, inst.InstanceVersion, v+2)
	}
}

// TestTodo_WF_RUN_010_OperatorCancelMapping pins how each governed decision
// reaches the control result.
func TestTodo_WF_RUN_010_OperatorCancelMapping(t *testing.T) {
	cases := []struct {
		out     cancellation.Outcome
		outcome Outcome
		code    string
	}{
		{cancellation.Outcome{Decision: workflow.Cancelled}, OutcomeApplied, ""},
		{cancellation.Outcome{Decision: workflow.CompensationRequired, Reasons: []cancellation.Reason{{Code: cancellation.ReasonEffectCompensation, NodeID: "n"}}}, OutcomeApplied, CodeCompensationRequired},
		{cancellation.Outcome{Decision: workflow.CannotCancel, Reasons: []cancellation.Reason{{Code: cancellation.ReasonEffectIrreversible, NodeID: "n"}}}, OutcomeTooLate, CodeEffectCommitted},
		{cancellation.Outcome{Decision: workflow.CannotCancel, Reasons: []cancellation.Reason{{Code: cancellation.ReasonChildNotCancellable}}}, OutcomeTooLate, CodeChildNotCancellable},
		{cancellation.Outcome{Decision: workflow.RepairRequired, Reasons: []cancellation.Reason{{Code: cancellation.ReasonEffectAmbiguous}}}, OutcomeRepairRequired, CodeEffectInFlight},
		{cancellation.Outcome{Decision: workflow.RepairRequired, Reasons: []cancellation.Reason{{Code: cancellation.ReasonChildRepair}}}, OutcomeRepairRequired, cancellation.ReasonChildRepair},
		{cancellation.Outcome{Decision: workflow.RepairRequired}, OutcomeRepairRequired, CodeEffectInFlight},
	}
	for _, tc := range cases {
		got := cancelResult(tc.out)
		if got.Outcome != tc.outcome || got.Code != tc.code || !got.durable {
			t.Fatalf("%s -> %+v, want %s %s durable", tc.out.Decision, got, tc.outcome, tc.code)
		}
	}
}
