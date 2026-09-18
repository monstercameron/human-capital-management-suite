package promotionsteps

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

type holdReleaseFake struct {
	err    error
	status string
}

func (f holdReleaseFake) ReleaseHold(_ context.Context, _ execute.StepRequest) (HoldReleaseResult, error) {
	status := f.status
	if status == "" {
		status = "COMPENSATED"
	}
	return HoldReleaseResult{Artifact: Artifact{OutputDigest: "sha256:hold-release"}, Status: status}, f.err
}

func compensateHoldRequest(t *testing.T) execute.StepRequest {
	t.Helper()
	plan, err := promotionexec.Compile()
	if err != nil {
		t.Fatalf("promotionexec.Compile: %v", err)
	}
	node, ok := plan.Node(promotionexec.NodeCompensateHold)
	if !ok {
		t.Fatalf("compiled plan carries no node %q", promotionexec.NodeCompensateHold)
	}
	return execute.StepRequest{Node: node, Plan: plan}
}

func TestPromotionCompensateNodeWithoutPortFailsClosed(t *testing.T) {
	runner := New(Config{})
	req := compensateHoldRequest(t)
	outcome, _, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run(compensate) = %v", err)
	}
	if !outcome.Failed || outcome.ErrorClass != FailureNotWired {
		t.Fatalf("outcome = %+v, want failed PORT_NOT_CONFIGURED: an unwired correction must not run", outcome)
	}
}

func TestPromotionCompensateNodeRoutesPortVerdict(t *testing.T) {
	for _, status := range []workflow.Outcome{"COMPENSATED", "PARTIAL", "FAILED", "REPAIR_REQUIRED"} {
		runner := New(Config{CompensateHold: holdReleaseFake{status: string(status)}})
		req := compensateHoldRequest(t)
		outcome, _, err := runner.Run(context.Background(), req)
		if err != nil {
			t.Fatalf("Run(compensate=%s) = %v", status, err)
		}
		if outcome.Failed {
			t.Fatalf("compensate outcome failed with class %q for port status %q", outcome.ErrorClass, status)
		}
		if outcome.Outcome != status {
			t.Fatalf("compensate route = %q, want %q: the port verdict, not a reinterpretation", outcome.Outcome, status)
		}
		if outcome.OutputDigest == "" {
			t.Fatalf("compensate outcome carries no output digest for port status %q", status)
		}
	}
}

func TestPromotionCompensateNodeRejectsUnknownVerdict(t *testing.T) {
	runner := New(Config{CompensateHold: holdReleaseFake{status: "SUCCEEDED"}})
	req := compensateHoldRequest(t)
	outcome, _, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run(compensate) = %v", err)
	}
	if !outcome.Failed || outcome.ErrorClass != FailureBadOutput {
		t.Fatalf("outcome = %+v, want failed OUTPUT_DIGEST_MISSING-class failure: SUCCEEDED is not a compensate route", outcome)
	}
}
