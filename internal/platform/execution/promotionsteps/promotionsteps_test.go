package promotionsteps

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/localcommit"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/revalidate"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

type snapshotFake struct{ err error }

func (f snapshotFake) Snapshot(context.Context, execute.StepRequest) (Artifact, error) {
	return Artifact{OutputDigest: "sha256:snapshot"}, f.err
}

type compensationFake struct{ err error }

func (f compensationFake) SimulateCompensation(context.Context, execute.StepRequest) (Artifact, error) {
	return Artifact{OutputDigest: "sha256:compensation"}, f.err
}

type bandFake struct{ err error }

func (f bandFake) EvaluateBand(context.Context, execute.StepRequest) (Artifact, error) {
	return Artifact{OutputDigest: "sha256:band"}, f.err
}

type thresholdFake struct {
	err    error
	result ThresholdResult
}

func (f thresholdFake) RaiseThreshold(context.Context, execute.StepRequest) (ThresholdResult, error) {
	if f.result.OutputDigest == "" {
		f.result.OutputDigest = "sha256:threshold"
	}
	return f.result, f.err
}

type revalidateFake struct{ err error }

func (f revalidateFake) Revalidate(context.Context, execute.StepRequest) (RevalidationResult, error) {
	return RevalidationResult{Artifact: Artifact{OutputDigest: "sha256:revalidate"}, Confirmed: true}, f.err
}

type validityFake struct {
	err    error
	status string
}

func (f validityFake) StillValid(context.Context, execute.StepRequest) (ValidityResult, error) {
	status := f.status
	if status == "" {
		status = "CONFIRMED"
	}
	return ValidityResult{Artifact: Artifact{OutputDigest: "sha256:validity"}, Status: status}, f.err
}

type executeFake struct {
	err   error
	calls int
}

type preparedPlanFake struct {
	prepared localcommit.PreparedPlan
	calls    int
}

func (f *preparedPlanFake) PreparePromotion(context.Context, execute.StepRequest, string) (localcommit.PreparedPlan, error) {
	return f.prepared, nil
}

func (f *preparedPlanFake) ExecutePreparedPromotion(context.Context, execute.StepRequest, localcommit.PreparedPlan) (Artifact, error) {
	f.calls++
	return Artifact{OutputDigest: "sha256:prepared-commit"}, nil
}

func (f *executeFake) ExecutePromotion(context.Context, execute.StepRequest, string) (Artifact, error) {
	f.calls++
	return Artifact{OutputDigest: "sha256:commit"}, f.err
}

type observationFake struct{ err error }

func (f observationFake) Observe(context.Context, execute.StepRequest) (ObservationResult, error) {
	return ObservationResult{Artifact: Artifact{OutputDigest: "sha256:observed"}, Status: ObservationObserved}, f.err
}

type reconciliationFake struct{ err error }

func (f reconciliationFake) Reconcile(context.Context, execute.StepRequest) (ReconciliationResult, error) {
	return ReconciliationResult{Artifact: Artifact{OutputDigest: "sha256:reconciled"}, Status: ReconciliationConsistent}, f.err
}

func TestPromotionStepsCoverEveryNodeOfTheCompiledPlan(t *testing.T) {
	plan, err := promotionexec.Compile()
	if err != nil {
		t.Fatalf("promotionexec.Compile: %v", err)
	}
	runner := New(Config{})
	for _, node := range plan.Nodes {
		if !runner.HandlesNode(node.ID) {
			t.Errorf("runner has no dispatch case for compiled node %s", node.ID)
		}
	}
}

func TestPromotionStepsDispatchesTypedOutcomes(t *testing.T) {
	commit := &executeFake{}
	runner := New(Config{
		SnapshotWorker:        snapshotFake{},
		SimulateCompensation:  compensationFake{},
		EvaluateBand:          bandFake{},
		RaiseThreshold:        thresholdFake{result: ThresholdResult{Route: workflow.Outcome("ABOVE_THRESHOLD")}},
		Revalidate:            revalidateFake{},
		StillValid:            validityFake{},
		ExecutePromotion:      commit,
		ObservePayroll:        observationFake{},
		ObserveAccess:         observationFake{},
		ObserveReconciliation: reconciliationFake{},
	})
	plan, err := promotionexec.Compile()
	if err != nil {
		t.Fatalf("promotionexec.Compile: %v", err)
	}

	for _, node := range plan.Nodes {
		req := stepRequest(node)
		out, _, runErr := runner.Run(context.Background(), req)
		switch node.ID {
		case promotionexec.NodeWaitEffectiveDate:
			if !errors.Is(runErr, ErrWaitOwnedByDriver) {
				t.Errorf("%s error = %v, want ErrWaitOwnedByDriver", node.ID, runErr)
			}
		case promotionexec.NodeApproveFinance, promotionexec.NodeApproveManager, promotionexec.NodeReapproval:
			if runErr != nil || out.Await != frontier.AwaitWorkItem {
				t.Errorf("%s = outcome=%+v err=%v, want WORK_ITEM_REQUIRED", node.ID, out, runErr)
			}
		case promotionexec.NodeEndComplete, promotionexec.NodeEndRepairPlan,
			promotionexec.NodeEndRejected, promotionexec.NodeEndInvalidated,
			promotionexec.NodeEndExpired, promotionexec.NodeEndCancelled,
			promotionexec.NodeEndBlocked:
			if runErr != nil || out.NodeID != node.ID || out.Outcome != "" {
				t.Errorf("%s = %+v err=%v, want driver-owned bare outcome", node.ID, out, runErr)
			}
		default:
			if runErr != nil || out.NodeID != node.ID || out.OutputDigest == "" || out.Failed {
				t.Errorf("%s = %+v err=%v, want typed successful output", node.ID, out, runErr)
			}
		}
	}
}

func TestPromotionStepsDuplicateExecutePromotionWritesOnce(t *testing.T) {
	commit := &executeFake{}
	runner := New(Config{ExecutePromotion: commit})
	req := stepRequest(workflow.CompiledNode{ID: promotionexec.NodeExecutePromotion, Type: workflow.StepCapability})
	req.Proposal = runtime.ProposalBinding{Revision: intent.ProposalRevision{
		MaterialDigest: digest.Reference{Digest: "sha256:proposal"},
	}}

	first, _, err := runner.Run(context.Background(), req)
	if err != nil || first.Failed {
		t.Fatalf("first execute_promotion = %+v, err=%v", first, err)
	}
	second, _, err := runner.Run(context.Background(), req)
	if err != nil || second.Failed {
		t.Fatalf("duplicate execute_promotion = %+v, err=%v", second, err)
	}
	if commit.calls != 1 {
		t.Fatalf("execute promotion calls = %d, want 1", commit.calls)
	}
	if second.OutputDigest != first.OutputDigest {
		t.Fatalf("replay digest = %q, want %q", second.OutputDigest, first.OutputDigest)
	}
}

func TestTodo_PROMO_005(t *testing.T) {
	prepared := &preparedPlanFake{}
	runner := New(Config{PreparePromotion: prepared, ExecutePreparedPromotion: prepared})
	req := stepRequest(workflow.CompiledNode{ID: promotionexec.NodeExecutePromotion, Type: workflow.StepCapability})
	first, _, err := runner.Run(context.Background(), req)
	if err != nil || first.Failed || first.OutputDigest != "sha256:prepared-commit" {
		t.Fatalf("prepared execute_promotion = %+v, err=%v", first, err)
	}
	if prepared.calls != 1 {
		t.Fatalf("prepared terminal calls = %d, want 1", prepared.calls)
	}
}

func TestPromotionStepsPortFailuresAreTypedAndNodeNamed(t *testing.T) {
	fault := errors.New("port unavailable")
	cases := []struct {
		name string
		node string
		cfg  Config
	}{
		{"snapshot", promotionexec.NodeSnapshotWorker, Config{SnapshotWorker: snapshotFake{err: fault}}},
		{"compensation", promotionexec.NodeSimulateCompensation, Config{SimulateCompensation: compensationFake{err: fault}}},
		{"band", promotionexec.NodeEvaluateBand, Config{EvaluateBand: bandFake{err: fault}}},
		{"threshold", promotionexec.NodeRaiseThreshold, Config{RaiseThreshold: thresholdFake{err: fault}}},
		{"revalidate", promotionexec.NodeRevalidate, Config{Revalidate: revalidateFake{err: fault}}},
		{"still_valid", promotionexec.NodeStillValid, Config{StillValid: validityFake{err: fault}}},
		{"execute", promotionexec.NodeExecutePromotion, Config{ExecutePromotion: &executeFake{err: fault}}},
		{"payroll", promotionexec.NodeObservePayroll, Config{ObservePayroll: observationFake{err: fault}}},
		{"access", promotionexec.NodeObserveAccess, Config{ObserveAccess: observationFake{err: fault}}},
		{"reconciliation", promotionexec.NodeObserveReconciliation, Config{ObserveReconciliation: reconciliationFake{err: fault}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := stepRequest(workflow.CompiledNode{ID: tc.node, Type: nodeType(tc.node)})
			if tc.node == promotionexec.NodeExecutePromotion {
				req.Proposal = runtime.ProposalBinding{Revision: intent.ProposalRevision{
					MaterialDigest: digest.Reference{Digest: "sha256:proposal"},
				}}
			}
			out, _, err := New(tc.cfg).Run(context.Background(), req)
			if err != nil {
				t.Fatalf("Run error = %v, want typed outcome", err)
			}
			if out.NodeID != tc.node || !out.Failed || out.ErrorClass != FailurePort {
				t.Fatalf("outcome = %+v, want node-named %s failure", out, FailurePort)
			}
		})
	}
}

func TestPromotionStepsMapGovernanceAndObservationRoutes(t *testing.T) {
	runner := New(Config{
		RaiseThreshold:        thresholdFake{result: ThresholdResult{Tier: rules.ApprovalTierStandard}},
		StillValid:            validityFake{},
		ObservePayroll:        observationFake{},
		ObserveReconciliation: reconciliationFake{},
	})
	checks := []struct {
		node workflow.CompiledNode
		want workflow.Outcome
	}{
		{workflow.CompiledNode{ID: promotionexec.NodeRaiseThreshold, Type: workflow.StepDecision}, workflow.Outcome("WITHIN_THRESHOLD")},
		{workflow.CompiledNode{ID: promotionexec.NodeStillValid, Type: workflow.StepDecision}, workflow.Outcome("VALID")},
		{workflow.CompiledNode{ID: promotionexec.NodeObservePayroll, Type: workflow.StepObserve}, workflow.Outcome("OBSERVED")},
		{workflow.CompiledNode{ID: promotionexec.NodeObserveReconciliation, Type: workflow.StepObserve}, workflow.Outcome("CONSISTENT")},
	}
	for _, check := range checks {
		out, _, err := runner.Run(context.Background(), stepRequest(check.node))
		if err != nil {
			t.Fatalf("%s: %v", check.node.ID, err)
		}
		if out.Outcome != check.want {
			t.Errorf("%s outcome = %s, want %s", check.node.ID, out.Outcome, check.want)
		}
	}
}

func TestPromotionStepsMapAllStillValidRequirements(t *testing.T) {
	for _, tc := range []struct {
		status string
		want   workflow.Outcome
	}{
		{"CONFIRMED", workflow.Outcome("VALID")},
		{string(revalidate.RequirementReapprovalRequired), workflow.Outcome("REAPPROVAL_REQUIRED")},
		{string(revalidate.RequirementBlock), workflow.Outcome("BLOCKED")},
		{string(revalidate.RequirementReplanRequired), workflow.Outcome("BLOCKED")},
	} {
		runner := New(Config{StillValid: validityFake{status: tc.status}})
		req := stepRequest(workflow.CompiledNode{ID: promotionexec.NodeStillValid, Type: workflow.StepDecision})
		out, _, err := runner.Run(context.Background(), req)
		if err != nil || out.Outcome != tc.want {
			t.Errorf("status %s = outcome=%+v err=%v, want %s", tc.status, out, err, tc.want)
		}
	}
}

type statusObservation struct{ status string }

func (f statusObservation) Observe(context.Context, execute.StepRequest) (ObservationResult, error) {
	return ObservationResult{Artifact: Artifact{OutputDigest: "sha256:observed-" + f.status}, Status: f.status}, nil
}

// TestPromotionStepsRouteARealObservationFailure proves an observation that
// did not see the committed change routes the compiled FAIL edge, and a status
// outside the vocabulary is a typed failure, never PASS.
func TestPromotionStepsRouteARealObservationFailure(t *testing.T) {
	plan, err := promotionexec.Compile()
	if err != nil {
		t.Fatalf("promotionexec.Compile: %v", err)
	}
	node, ok := plan.Node(promotionexec.NodeObservePayroll)
	if !ok {
		t.Fatal("compiled plan has no observe_payroll")
	}
	out, _, err := New(Config{ObservePayroll: statusObservation{status: ObservationFailed}}).Run(context.Background(), stepRequest(node))
	if err != nil || out.Failed || out.Outcome != workflow.OutcomeFail || out.OutputDigest != "sha256:observed-FAIL" {
		t.Fatalf("FAIL observation = %+v, %v; want the FAIL route", out, err)
	}
	out, _, err = New(Config{ObservePayroll: statusObservation{status: "MAYBE"}}).Run(context.Background(), stepRequest(node))
	if err != nil || !out.Failed || out.ErrorClass != FailureBadOutput {
		t.Fatalf("unknown observation status = %+v, %v; want %s", out, err, FailureBadOutput)
	}
	// A node without a compiled FAIL route cannot take one.
	bare := workflow.CompiledNode{ID: promotionexec.NodeObservePayroll, Type: workflow.StepObserve}
	out, _, _ = New(Config{ObservePayroll: statusObservation{status: ObservationFailed}}).Run(context.Background(), stepRequest(bare))
	if !out.Failed || out.ErrorClass != FailureBadOutput {
		t.Fatalf("FAIL on a node without the route = %+v; want %s", out, FailureBadOutput)
	}
}

func stepRequest(node workflow.CompiledNode) execute.StepRequest {
	return execute.StepRequest{
		TenantID:      uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		InstanceID:    uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		Attempt:       1,
		Node:          node,
		Proposal:      runtime.ProposalBinding{Revision: intent.ProposalRevision{MaterialDigest: digest.Reference{Digest: "sha256:proposal"}}},
		RecordedAt:    time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC),
		CorrelationID: "correlation:promotion",
	}
}

func nodeType(node string) workflow.StepType {
	switch node {
	case promotionexec.NodeRaiseThreshold, promotionexec.NodeStillValid:
		return workflow.StepDecision
	case promotionexec.NodeObservePayroll, promotionexec.NodeObserveAccess, promotionexec.NodeObserveReconciliation:
		return workflow.StepObserve
	default:
		return workflow.StepCapability
	}
}
