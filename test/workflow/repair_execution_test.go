package workflow_test

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/reconcile"
	operationrepair "github.com/monstercameron/human-capital-management-suite/internal/operations/repair"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

type promotionRepairEffect struct{ calls []execute.RepairEffectRequest }

func (p *promotionRepairEffect) ExecuteRepairEffect(_ context.Context, req execute.RepairEffectRequest) (execute.RepairEffectResult, error) {
	p.calls = append(p.calls, req)
	return execute.RepairEffectResult{EffectKey: req.Step.EffectKey, EffectRef: req.Step.EffectRef, Accepted: true, ResultRef: "iam:accepted"}, nil
}

type promotionRepairObserver struct{}

func (promotionRepairObserver) ObserveRepair(context.Context, operationrepair.RepairPlan, execute.RepairEffectResult) (execute.RepairObservation, error) {
	return execute.RepairObservation{Observed: true, Complete: true, Digest: "sha256:iam-observed", State: "IAM_EXPECTED"}, nil
}

type promotionRepairVerifier struct{}

func (promotionRepairVerifier) ReconcileRepair(context.Context, execute.RepairReconciliationRequest) (reconcile.CompletionDecision, error) {
	return reconcile.CompletionDecision{Status: reconcile.CompletionPass, Terminal: true, Route: reconcile.RouteConsistent, Reason: "fresh complete observation matches intended and canonical values"}, nil
}

// TestTodo_WF_RUN_016_Integration starts with the shipped Promotion fixture's
// end_repair_plan terminal, then runs the failed effect under the independent
// REPAIR mode and proves the original semantic identity survives to the one
// redrive before RECON-002 closes consistency.
func TestTodo_WF_RUN_016_Integration(t *testing.T) {
	f := newPromotionFullFixture(t, "promotion-repair-mode", promotionFullBehavior{validity: []string{"VALID"}, payroll: "FAIL"})
	_, parked := runPromotionToWait(t, f)
	if got, err := makePromotionScheduler(t, f, f.fireAt).Tick(context.Background()); err != nil || got.Completed != 1 {
		t.Fatalf("Promotion repair terminal tick = %+v, err=%v", got, err)
	}
	// The committed run parks on the payroll provider's confirmation; the
	// confirmation resumes it into the observation that reports FAIL.
	if result := confirmProviderWait(t, f, parked.Start.InstanceID, promotionexec.NodeAwaitPayrollConfirmation, "hcmnext.integrations.payroll", f.fireAt); result.Status != execute.StatusComplete {
		t.Fatalf("after the payroll confirmation the run = %+v, want it COMPLETE on RepairPlan", result)
	}
	assertPromotionRows(t, f, parked.Start.InstanceID, "REPAIR_REQUIRED", "end_repair_plan", 1)

	plan := operationrepair.RepairPlan{
		ID: "repair:" + f.key, Digest: "sha256:repair-promotion-mode", FindingDigest: "sha256:payroll-finding",
		ObservationDigest: "sha256:payroll-observation", AuthorityPolicy: "authority.payroll/1",
		MappingVersion: "mapping.payroll/1", CredentialRef: "credential:payroll/1", TargetVersion: "payroll:worker@7",
		OriginalSemanticKey: "promotion:" + f.key, FailedEffectKey: "effect:payroll-provision",
		Steps: []operationrepair.Step{{Ordinal: 1, EffectKey: "effect:payroll-provision", EffectRef: "operation:payroll:" + f.key, Target: "payroll:worker", ExpectedVersion: "payroll:worker@7", MaxAttempts: 2}},
	}
	current := operationrepair.CurrentEvidence{
		PlanDigest: plan.Digest, FindingDigest: plan.FindingDigest, ObservationDigest: plan.ObservationDigest,
		AuthorityPolicy: plan.AuthorityPolicy, MappingVersion: plan.MappingVersion, CredentialRef: plan.CredentialRef,
		TargetVersion: plan.TargetVersion,
	}
	effect := &promotionRepairEffect{}
	records := execute.NewMemoryRepairRecords()
	runner, err := execute.NewRepairExecutor(execute.RepairExecutionOptions{
		Admission: operationrepair.NewMemoryStore(), Effect: effect,
		Observation: promotionRepairObserver{}, Reconciliation: promotionRepairVerifier{},
		Records: records,
	})
	if err != nil {
		t.Fatal(err)
	}
	repairRequest := execute.RepairExecutionRequest{
		TenantID: f.tenantID, Plan: plan, Current: current,
		Now: f.fireAt.Add(time.Hour), Actor: "operator:repair", Author: "analyst:reconciliation",
	}
	result, err := runner.Execute(context.Background(), repairRequest)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != execute.RepairCompleted || !result.Executed || result.ConsistencyState != "CONSISTENT" || result.Reconciliation.Route != reconcile.RouteConsistent {
		t.Fatalf("repair result = %+v, want completed/consistent", result)
	}
	if len(effect.calls) != 1 || effect.calls[0].OriginalSemanticKey != plan.OriginalSemanticKey || effect.calls[0].Step.EffectKey != plan.FailedEffectKey {
		t.Fatalf("redrive calls = %+v, want only failed effect with original semantic key", effect.calls)
	}

	// The durable record, not this executor's memory, is what stops a second
	// redrive: a freshly composed executor sharing only the record replays the
	// settled decision and never reaches the connector again.
	restarted, err := execute.NewRepairExecutor(execute.RepairExecutionOptions{
		Admission: operationrepair.NewMemoryStore(), Effect: effect,
		Observation: promotionRepairObserver{}, Reconciliation: promotionRepairVerifier{},
		Records: records,
	})
	if err != nil {
		t.Fatal(err)
	}
	replayRequest := repairRequest
	replayRequest.Now = f.fireAt.Add(2 * time.Hour)
	replayed, err := restarted.Execute(context.Background(), replayRequest)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Status != execute.RepairCompleted || !replayed.Executed || replayed.ConsistencyState != "CONSISTENT" {
		t.Fatalf("replayed repair = %+v, want the recorded COMPLETED decision", replayed)
	}
	if len(effect.calls) != 1 {
		t.Fatalf("redrive calls after a restart = %d, want the one original redrive", len(effect.calls))
	}
}
