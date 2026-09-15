package app

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	stepsapproval "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/approval"
)

// kernelExecutor is a ProposalExecutor with an approval kernel that records
// what the journey handed it.
type kernelExecutor struct {
	plainExecutor
	vote       ApprovalVoteRequest
	invalidate ApprovalInvalidationRequest
	voted      ApprovalVoteResult
	err        error
}

func (k *kernelExecutor) CompleteApproval(_ context.Context, req ApprovalVoteRequest) (ApprovalVoteResult, error) {
	k.vote = req
	return k.voted, k.err
}

func (k *kernelExecutor) InvalidateApproval(_ context.Context, req ApprovalInvalidationRequest) (ExecutionResult, error) {
	k.invalidate = req
	return ExecutionResult{InstanceID: req.InstanceID.String(), Status: ExecutionResultComplete}, k.err
}

// TestTodo_WF_STEP_018_JourneyUsesTheKernel proves the served decision is
// handed to the approval kernel with the journey's own hooks, that a replay
// carries none, and that a cell without a kernel cannot decide at all.
func TestTodo_WF_STEP_018_JourneyUsesTheKernel(t *testing.T) {
	item, revision, decision, at := journeyRoutedFixtures(t, true)
	item.Status, item.CompletedBy, item.CompletedOutputDigest, item.CompletedAt = workitem.StatusAssigned, "", "", nil
	start := runtime.StartRequest{Proposal: runtime.ProposalBinding{Revision: revision}}
	instance := runtime.Instance{InstanceID: item.WorkflowInstanceID, InstanceVersion: 7}

	t.Run("a fresh vote carries the recheck, claim and evidence hooks", func(t *testing.T) {
		kernel := &kernelExecutor{voted: ApprovalVoteResult{Item: item, Execution: ExecutionResult{Status: ExecutionResultParked}}}
		engine := newJourneyEngine(&IntentService{executor: kernel}, nil, "", nil, nil)
		done, err := engine.voteThroughKernel(context.Background(), journeyVote{
			start: start, instance: instance, item: item, decision: decision, at: at,
		})
		if err != nil {
			t.Fatalf("voteThroughKernel: %v", err)
		}
		req := kernel.vote
		if req.WorkItemID != item.WorkItemID || req.ExpectedInstanceVersion != 7 || req.ExpectedWorkItemVersion != item.ItemVersion ||
			req.Decision.Digest() != decision.Digest() || req.Continuation.NodeID != prototype.NodeApproval || req.Continuation.Digest == "" {
			t.Fatalf("kernel request = %+v", req)
		}
		if req.Recheck == nil || req.Prepare == nil || req.Record == nil {
			t.Fatal("a fresh vote must carry the recheck, claim and evidence hooks")
		}
		if err := req.Recheck(context.Background(), nil, item); !errors.Is(err, ErrPromotionAuthorityStale) {
			t.Fatalf("recheck without a verified principal = %v, want ErrPromotionAuthorityStale", err)
		}
		if !done.settled || !done.executed || done.needsResume() || done.execution.Status != ExecutionResultParked {
			t.Fatalf("decided = %+v, want a settled vote with its execution", done)
		}
	})

	t.Run("a replayed vote carries no hooks and a pending vote no execution", func(t *testing.T) {
		kernel := &kernelExecutor{voted: ApprovalVoteResult{Item: item, Pending: true}}
		engine := newJourneyEngine(&IntentService{executor: kernel}, nil, "", nil, nil)
		done, err := engine.voteThroughKernel(context.Background(), journeyVote{
			start: start, instance: instance, item: item, decision: decision, at: at, replay: true,
		})
		if err != nil {
			t.Fatalf("voteThroughKernel(replay): %v", err)
		}
		if kernel.vote.Recheck != nil || kernel.vote.Prepare != nil || kernel.vote.Record != nil {
			t.Fatal("a replayed vote must not run the fresh-vote hooks")
		}
		if !done.settled || done.executed || !done.replayed || done.needsResume() {
			t.Fatalf("decided = %+v, want a settled pending replay", done)
		}
	})

	t.Run("a stale authority is invalidated through the kernel", func(t *testing.T) {
		kernel := &kernelExecutor{}
		engine := newJourneyEngine(&IntentService{executor: kernel}, nil, "", nil, nil)
		done, err := engine.invalidateThroughKernel(context.Background(), start, instance, item, "principal:manager", at, "the manager changed")
		if err != nil {
			t.Fatalf("invalidateThroughKernel: %v", err)
		}
		req := kernel.invalidate
		if req.Change != humanwork.InvalidatorAuthorityRevoked || req.Meta.Reason != journeyReasonInvalidated ||
			req.Meta.ActorPrincipalID != "principal:manager" || req.Meta.Detail != "the manager changed" || len(req.Requirements.Requirements) != 1 {
			t.Fatalf("invalidation request = %+v", req)
		}
		if !done.executed || !errors.Is(done.refusal, ErrProposalDecisionInvalidated) {
			t.Fatalf("decided = %+v, want an executed invalidation reporting the refusal", done)
		}
	})

	t.Run("a kernel refusal travels in the decision vocabulary", func(t *testing.T) {
		kernel := &kernelExecutor{err: stepsapproval.ErrDuplicateApprover}
		engine := newJourneyEngine(&IntentService{executor: kernel}, nil, "", nil, nil)
		_, err := engine.voteThroughKernel(context.Background(), journeyVote{start: start, instance: instance, item: item, decision: decision, at: at})
		if !errors.Is(err, ErrProposalDecisionSeparation) {
			t.Fatalf("duplicate approver = %v, want ErrProposalDecisionSeparation", err)
		}
		if _, err := engine.invalidateThroughKernel(context.Background(), start, instance, item, "p", at, "d"); !errors.Is(err, ErrProposalDecisionSeparation) {
			t.Fatalf("invalidation refusal = %v, want the mapped refusal", err)
		}
	})

	t.Run("a cell without a kernel cannot decide", func(t *testing.T) {
		engine := newJourneyEngine(&IntentService{executor: plainExecutor{}}, nil, "", nil, nil)
		if _, err := engine.voteThroughKernel(context.Background(), journeyVote{start: start, instance: instance, item: item, decision: decision, at: at}); !errors.Is(err, ErrProposalDecisionUnavailable) {
			t.Fatalf("vote without a kernel = %v, want ErrProposalDecisionUnavailable", err)
		}
		if _, err := engine.invalidateThroughKernel(context.Background(), start, instance, item, "p", at, "d"); !errors.Is(err, ErrProposalDecisionUnavailable) {
			t.Fatalf("invalidation without a kernel = %v, want ErrProposalDecisionUnavailable", err)
		}
	})

	t.Run("a continuation that cannot be rebuilt is refused before the kernel", func(t *testing.T) {
		kernel := &kernelExecutor{}
		engine := newJourneyEngine(&IntentService{executor: kernel}, nil, "", nil, nil)
		rerouted := item
		rerouted.Assignment.Resolution.Candidates = []humanwork.Candidate{{PrincipalID: "principal:somebody-else", Via: humanwork.SourceDirect}}
		if _, err := engine.voteThroughKernel(context.Background(), journeyVote{start: start, instance: instance, item: rerouted, decision: decision, at: at}); err == nil {
			t.Fatal("a vote on an item whose requirement cannot be rebuilt reached the kernel")
		}
		if _, err := engine.invalidateThroughKernel(context.Background(), start, instance, rerouted, "p", at, "d"); err == nil {
			t.Fatal("an invalidation of an item whose requirement cannot be rebuilt reached the kernel")
		}
		if kernel.vote.WorkItemID == item.WorkItemID || kernel.invalidate.InstanceID == instance.InstanceID {
			t.Fatal("the kernel was called for an unrebuildable continuation")
		}
	})
}

// TestTodo_WF_STEP_018_ApprovalKernelError pins the projection of kernel
// errors onto the decision vocabulary.
func TestTodo_WF_STEP_018_ApprovalKernelError(t *testing.T) {
	if err := approvalKernelError(ErrProposalDecisionRoute); !errors.Is(err, ErrProposalDecisionRoute) {
		t.Fatalf("owned refusal = %v, want it unchanged", err)
	}
	for _, sod := range []error{stepsapproval.ErrSeparationConflict, stepsapproval.ErrDuplicateApprover} {
		if err := approvalKernelError(sod); !errors.Is(err, ErrProposalDecisionSeparation) || !errors.Is(err, sod) {
			t.Fatalf("%v = %v, want ErrProposalDecisionSeparation keeping the cause", sod, err)
		}
	}
	if err := approvalKernelError(workitemRefusalFixture(t)); !errors.Is(err, workspace.ErrJourneyStage) {
		t.Fatalf("store refusal = %v, want the work item projection", err)
	}
	var owned *envelope.Error
	if err := approvalKernelError(errors.New("driver fault")); !errors.As(err, &owned) {
		t.Fatalf("driver fault = %v, want an execution envelope", err)
	}
	if got := proposalDecisionError(owned); got != owned {
		t.Fatalf("proposalDecisionError re-wrapped an execution envelope: %v", got)
	}
}
