package promotionsteps

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

// TestRulesThresholdPortResolvesTenantTables proves REV-010-02 at the
// execution port: a published tenant resolution evaluates the tenant own
// table (same increase, tenant tier, cited tenant version), a resolution
// failure fails the step, and a malformed published override fails the step
// instead of answering the reference table.
func TestRulesThresholdPortResolvesTenantTables(t *testing.T) {
	inputs := func(context.Context, execute.StepRequest) (rules.PromotionApprovalInput, error) {
		return rules.PromotionApprovalInput{
			IncreasePercent: values.MustDecimal("18.0000", 4, values.RoundingHalfEven),
			BandPosition:    rules.BandPositionInBand,
			BudgetAuthority: rules.BudgetAuthoritySufficient,
		}, nil
	}
	resolve := func(res rules.TenantThresholdResolution, err error) func(context.Context, execute.StepRequest) (rules.TenantThresholdResolution, error) {
		return func(context.Context, execute.StepRequest) (rules.TenantThresholdResolution, error) { return res, err }
	}
	req := stepRequest(workflow.CompiledNode{ID: promotionexec.NodeRaiseThreshold, Type: workflow.StepDecision})
	req.Context.Tenant = "tenant-a"

	got, err := RulesThresholdPort{
		Inputs: inputs,
		Thresholds: resolve(rules.TenantThresholdResolution{
			FinanceText: "5.0000", ExecutiveText: "15.0000", Revision: 7, DefinitionVersion: "tenant-params/v7", Found: true,
		}, nil),
	}.RaiseThreshold(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tier != rules.ApprovalTierExecutiveRequired || got.Route != "ABOVE_THRESHOLD" {
		t.Fatalf("tenant 18 percent = %+v, want EXECUTIVE/ABOVE_THRESHOLD", got)
	}
	if got.Decision.TableVersion != "tenant-params/v7" {
		t.Fatalf("cited version = %q", got.Decision.TableVersion)
	}

	fault := errors.New("parameters unavailable")
	if _, err := (RulesThresholdPort{Inputs: inputs, Thresholds: resolve(rules.TenantThresholdResolution{}, fault)}).RaiseThreshold(context.Background(), req); !errors.Is(err, fault) {
		t.Fatalf("resolution failure = %v, want it returned", err)
	}
	malformed := RulesThresholdPort{Inputs: inputs, Thresholds: resolve(rules.TenantThresholdResolution{
		FinanceText: "30.0000", ExecutiveText: "20.0000", Revision: 3, DefinitionVersion: "tenant-params/v3", Found: true,
	}, nil)}
	if _, err := malformed.RaiseThreshold(context.Background(), req); err == nil {
		t.Fatal("malformed published override was evaluated")
	}
	unpublished, err := RulesThresholdPort{Inputs: inputs, Thresholds: resolve(rules.TenantThresholdResolution{}, nil)}.RaiseThreshold(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if unpublished.Tier != rules.ApprovalTierFinanceRequired || unpublished.Decision.TableVersion != rules.PromotionApprovalTableVersion {
		t.Fatalf("unpublished tenant = %+v, want reference FINANCE", unpublished)
	}
}
