package promotionsteps

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/rulethreshold"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// recordThresholdPort is the ThresholdPort double the record tests drive
// Runner.RunInTx through: it answers one canned evaluation with freezable
// inputs, or one canned port failure.
type recordThresholdPort struct {
	result ThresholdResult
	err    error
}

func (p recordThresholdPort) RaiseThreshold(context.Context, execute.StepRequest) (ThresholdResult, error) {
	return p.result, p.err
}

func recordFrozenInput(t *testing.T) rules.PromotionApprovalInput {
	t.Helper()
	increase, err := values.NewDecimal("12.50", 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return rules.PromotionApprovalInput{
		IncreasePercent: increase,
		BandPosition:    rules.BandPositionInBand,
		BudgetAuthority: rules.BudgetAuthoritySufficient,
		GradeChange:     true,
	}
}

func recordFrozenResult(in rules.PromotionApprovalInput) ThresholdResult {
	return ThresholdResult{
		Artifact: Artifact{OutputDigest: "sha256:threshold"},
		Tier:     rules.ApprovalTierFinanceRequired,
		Inputs:   in,
		Decision: rules.PromotionApprovalDecision{
			Tier:         rules.ApprovalTierFinanceRequired,
			MatchedRowID: "row-finance-1",
			TableID:      rules.PromotionApprovalTableID,
			TableVersion: rules.PromotionApprovalTableVersion,
			TableDigest:  "sha256:table",
		},
	}
}

func recordRequest(intentID string, mode workflow.ExecutionMode) execute.StepRequest {
	req := stepRequest(workflow.CompiledNode{ID: promotionexec.NodeRaiseThreshold, Type: workflow.StepDecision})
	req.Context = runtime.ExecutionContext{ExecutionMode: mode}
	req.Proposal.Revision.IntentID = intentID
	req.Proposal.Revision.Revision = 7
	return req
}

// TestRunnerRunInTxFreezesTheThresholdDecision proves the served
// advancement writes the RULE-003 evaluation it just produced into the
// same transaction (REV-010-01): in EXECUTE mode the frozen decision
// carries the revision key, tier, row, table identity and inputs, while
// simulate runs, recorder-less compositions and ports without freezable
// inputs commit the same outcome with no record. A revision the
// intent-control tables cannot key on is a loud error, and a port
// failure keeps the stable failure outcome the Run dispatch had.
func TestRunnerRunInTxFreezesTheThresholdDecision(t *testing.T) {
	intentID := uuid.New()
	t.Run("execute mode freezes the evaluation", func(t *testing.T) {
		in := recordFrozenInput(t)
		var recorded []rulethreshold.Decision
		runner := New(Config{
			RaiseThreshold: recordThresholdPort{result: recordFrozenResult(in)},
			RecordThresholdDecision: func(_ context.Context, _ runtime.Executor, d rulethreshold.Decision) error {
				recorded = append(recorded, d)
				return nil
			},
		})
		out, _, err := runner.RunInTx(context.Background(), nil, recordRequest(intentID.String(), workflow.ModeExecute))
		if err != nil || out.Failed || out.Outcome != workflow.Outcome("ABOVE_THRESHOLD") {
			t.Fatalf("RunInTx = %+v, %v; want the ABOVE_THRESHOLD outcome", out, err)
		}
		if len(recorded) != 1 {
			t.Fatalf("recorded decisions = %d, want 1", len(recorded))
		}
		digest, err := rules.InputDigest(in)
		if err != nil {
			t.Fatal(err)
		}
		got := recorded[0]
		if got.TenantID != uuid.MustParse("11111111-1111-1111-1111-111111111111") || got.IntentID != intentID ||
			got.Revision != 7 || got.Attempt != 1 || got.InstanceID != uuid.MustParse("22222222-2222-2222-2222-222222222222") {
			t.Fatalf("record key = %+v, want the request's tenant/intent/revision/attempt/instance", got)
		}
		if got.Tier != string(rules.ApprovalTierFinanceRequired) || got.MatchedRow != "row-finance-1" ||
			got.TableID != rules.PromotionApprovalTableID || got.TableVersion != rules.PromotionApprovalTableVersion ||
			got.TableDigest != "sha256:table" || got.InputDigest != digest {
			t.Fatalf("frozen decision = %+v, want the evaluated tier/row/table", got)
		}
		if !got.Input.IncreasePercent.Equal(in.IncreasePercent) || got.Input.BandPosition != in.BandPosition ||
			got.Input.BudgetAuthority != in.BudgetAuthority || got.Input.GradeChange != in.GradeChange {
			t.Fatalf("frozen inputs = %+v, want %+v", got.Input, in)
		}
	})

	for _, tc := range []struct {
		name   string
		config Config
		req    execute.StepRequest
	}{
		{"simulate mode leaves no record", Config{RaiseThreshold: recordThresholdPort{result: recordFrozenResult(recordFrozenInput(t))}, RecordThresholdDecision: func(context.Context, runtime.Executor, rulethreshold.Decision) error {
			t.Error("simulate run froze approval-time evidence")
			return nil
		}}, recordRequest(intentID.String(), workflow.ModeSimulate)},
		{"recorder-less composition runs uncovered", Config{RaiseThreshold: recordThresholdPort{result: recordFrozenResult(recordFrozenInput(t))}}, recordRequest(intentID.String(), workflow.ModeExecute)},
		{"port without freezable inputs leaves no record", Config{RaiseThreshold: recordThresholdPort{result: ThresholdResult{Artifact: Artifact{OutputDigest: "sha256:threshold"}, Tier: rules.ApprovalTierStandard}}, RecordThresholdDecision: func(context.Context, runtime.Executor, rulethreshold.Decision) error {
			t.Error("unfreezable evaluation was recorded")
			return nil
		}}, recordRequest(intentID.String(), workflow.ModeExecute)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, _, err := New(tc.config).RunInTx(context.Background(), nil, tc.req)
			if err != nil || out.Failed {
				t.Fatalf("RunInTx = %+v, %v; want the evaluated outcome with no record", out, err)
			}
		})
	}

	t.Run("non-uuid intent is a loud error", func(t *testing.T) {
		runner := New(Config{
			RaiseThreshold:          recordThresholdPort{result: recordFrozenResult(recordFrozenInput(t))},
			RecordThresholdDecision: func(context.Context, runtime.Executor, rulethreshold.Decision) error { return nil },
		})
		if _, _, err := runner.RunInTx(context.Background(), nil, recordRequest("intent:retry:synthetic", workflow.ModeExecute)); err == nil {
			t.Fatal("a non-intent-keyed revision froze a threshold decision")
		}
	})

	t.Run("port failure keeps the stable failure outcome", func(t *testing.T) {
		runner := New(Config{RaiseThreshold: recordThresholdPort{err: context.DeadlineExceeded}})
		out, _, err := runner.RunInTx(context.Background(), nil, recordRequest(intentID.String(), workflow.ModeExecute))
		if err != nil || !out.Failed || out.ErrorClass != FailurePort {
			t.Fatalf("RunInTx = %+v, %v; want the PORT_FAILURE outcome", out, err)
		}
	})
}

// TestRunnerRunsInTransactionClaimsOnlyTheThresholdNode proves the
// transaction boundary the freeze relies on: the threshold node commits
// with its outcome, every other node keeps its pre-transaction
// evaluation.
func TestRunnerRunsInTransactionClaimsOnlyTheThresholdNode(t *testing.T) {
	runner := New(Config{})
	if !runner.RunsInTransaction(workflow.CompiledNode{ID: promotionexec.NodeRaiseThreshold, Type: workflow.StepDecision}) {
		t.Fatal("the threshold node advances outside the transaction it freezes in")
	}
	for _, node := range []workflow.CompiledNode{
		{ID: promotionexec.NodeSnapshotWorker, Type: workflow.StepCapability},
		{ID: promotionexec.NodeApproveFinance, Type: workflow.StepApproval},
	} {
		if runner.RunsInTransaction(node) {
			t.Fatalf("node %q joined the advancement transaction", node.ID)
		}
	}
}
