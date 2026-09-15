package promotionsteps

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

// TestRulesThresholdPortRoutesTheRealDecision proves the production RULE-003
// adapter evaluates the published table: a grade change routes
// ABOVE_THRESHOLD, a small in-band funded raise routes WITHIN_THRESHOLD, an
// unknown budget routes UNKNOWN, and a resolver failure is returned.
func TestRulesThresholdPortRoutesTheRealDecision(t *testing.T) {
	in := func(increase string, band rules.BandPosition, budget rules.BudgetAuthority, grade bool) RulesThresholdPort {
		return RulesThresholdPort{Inputs: func(context.Context, execute.StepRequest) (rules.PromotionApprovalInput, error) {
			return rules.PromotionApprovalInput{IncreasePercent: values.MustDecimal(increase, 4, values.RoundingHalfEven), BandPosition: band, BudgetAuthority: budget, GradeChange: grade}, nil
		}}
	}
	req := stepRequest(workflow.CompiledNode{ID: promotionexec.NodeRaiseThreshold, Type: workflow.StepDecision})
	for _, tc := range []struct {
		name string
		port RulesThresholdPort
		want workflow.Outcome
		tier rules.ApprovalTier
	}{
		{"grade change", in("8.8889", rules.BandPositionInBand, rules.BudgetAuthoritySufficient, true), "ABOVE_THRESHOLD", rules.ApprovalTierFinanceRequired},
		{"small raise", in("3.0000", rules.BandPositionInBand, rules.BudgetAuthoritySufficient, false), "WITHIN_THRESHOLD", rules.ApprovalTierStandard},
		{"unknown budget", in("3.0000", rules.BandPositionInBand, rules.BudgetAuthorityUnknown, false), workflow.OutcomeUnknown, rules.ApprovalTierUnknownBlocked},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.port.RaiseThreshold(context.Background(), req)
			if err != nil || got.Route != tc.want || got.Tier != tc.tier || !strings.HasPrefix(got.OutputDigest, "sha256:") || got.Refs.PolicyRef == "" {
				t.Fatalf("RaiseThreshold = %+v, %v; want %s/%s", got, err, tc.want, tc.tier)
			}
			out, _, err := New(Config{RaiseThreshold: tc.port}).Run(context.Background(), req)
			if err != nil || out.Outcome != tc.want {
				t.Fatalf("runner outcome = %+v, %v; want %s", out, err, tc.want)
			}
		})
	}
	fault := errors.New("inputs unavailable")
	if _, err := (RulesThresholdPort{Inputs: func(context.Context, execute.StepRequest) (rules.PromotionApprovalInput, error) {
		return rules.PromotionApprovalInput{}, fault
	}}).RaiseThreshold(context.Background(), req); !errors.Is(err, fault) {
		t.Fatalf("resolver failure = %v, want it returned", err)
	}
	if _, err := (RulesThresholdPort{}).RaiseThreshold(context.Background(), req); err == nil {
		t.Fatal("an unconfigured threshold port answered")
	}
	if _, err := in("3.0000", "", rules.BudgetAuthoritySufficient, false).RaiseThreshold(context.Background(), req); err == nil {
		t.Fatal("an unspecified band position was evaluated")
	}
}

func TestPromotionStepsHelpers(t *testing.T) {
	deadline := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for _, node := range []string{promotionexec.NodeApproveFinance, promotionexec.NodeApproveManager} {
		if req, err := CompileApprovalRequirement(node, "principal:approver", deadline); err != nil || req.RequirementID == "" {
			t.Fatalf("CompileApprovalRequirement(%s) = %+v, %v", node, req, err)
		}
	}
	if _, err := CompileApprovalRequirement(promotionexec.NodeRevalidate, "principal:approver", deadline); err == nil {
		t.Fatal("a non-approval node compiled an approval requirement")
	}
	if _, err := NewRecordedAt(execute.StepRequest{}); err == nil {
		t.Fatal("NewRecordedAt accepted no instant")
	}
	if at, err := NewRecordedAt(stepRequest(workflow.CompiledNode{})); err != nil || at.Time().IsZero() {
		t.Fatalf("NewRecordedAt = %v, %v", at, err)
	}
	if PromotionInputSnapshotDigest("a") == PromotionInputSnapshotDigest("b") {
		t.Fatal("snapshot digests collide")
	}
	if NewRunner(Config{}) == nil || NewStepRunner(Config{}) == nil || New(Config{}).HandlesNode("unknown") {
		t.Fatal("constructors or HandlesNode misbehave")
	}
	if _, _, err := New(Config{}).Run(context.Background(), stepRequest(workflow.CompiledNode{ID: "unknown"})); err == nil {
		t.Fatal("an unknown node was dispatched")
	}
	if _, _, err := New(Config{}).Run(context.Background(), stepRequest(workflow.CompiledNode{ID: promotionexec.NodeSnapshotWorker, Type: workflow.StepDecision})); err == nil {
		t.Fatal("a mistyped node was dispatched")
	}
}
